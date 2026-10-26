package buildpipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const entryArtifactFileName = "entry-artifact.json"

type viteManifestEntry struct {
	File           string   `json:"file"`
	Name           string   `json:"name"`
	Src            string   `json:"src"`
	IsEntry        bool     `json:"isEntry"`
	CSS            []string `json:"css"`
	Imports        []string `json:"imports"`
	DynamicImports []string `json:"dynamicImports"`
}

type frontendEntryArtifact struct {
	Icons  map[string]string `json:"icons,omitempty"`
	Entry  string            `json:"entry"`
	Styles []string          `json:"styles,omitempty"`
	Files  map[string]string `json:"files"`
}

func generateFrontendEntryArtifact(frontendRoot, outputDir string) (string, error) {
	manifestPath := filepath.Join(outputDir, ".vite", "manifest.json")
	payload, err := os.ReadFile(manifestPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("读取 Vite Manifest 失败: %w", err)
	}
	entries := map[string]viteManifestEntry{}
	if err := json.Unmarshal(payload, &entries); err != nil {
		return "", fmt.Errorf("解析 Vite Manifest 失败: %w", err)
	}
	entryKey, entry, err := singleViteEntry(entries)
	if err != nil {
		return "", err
	}
	// A trusted root library build has a single frontend.mjs entry and no
	// iframe page artifact. Only HTML/asset entries produce entry-artifact.json.
	if !strings.HasPrefix(entry.File, "assets/") {
		return "", nil
	}
	files, err := digestFrontendAssets(outputDir)
	if err != nil {
		return "", err
	}
	visited := map[string]bool{}
	var collect func(string) error
	collect = func(key string) error {
		if visited[key] {
			return nil
		}
		visited[key] = true
		chunk, exists := entries[key]
		if !exists {
			return fmt.Errorf("Vite Manifest 引用了未知 chunk %q", key)
		}
		for _, relative := range append([]string{chunk.File}, chunk.CSS...) {
			if _, exists := files[relative]; !exists {
				return fmt.Errorf("Vite Manifest 引用的输出文件不存在: %q", relative)
			}
		}
		for _, imported := range append(append([]string(nil), chunk.Imports...), chunk.DynamicImports...) {
			if err := collect(imported); err != nil {
				return err
			}
		}
		return nil
	}
	if err := collect(entryKey); err != nil {
		return "", err
	}
	styles := append([]string(nil), entry.CSS...)
	sort.Strings(styles)
	icons, err := readFrontendIcons(outputDir, files)
	if err != nil {
		return "", err
	}
	artifact := frontendEntryArtifact{Icons: icons, Entry: entry.File, Styles: styles, Files: files}
	encoded, err := json.MarshalIndent(artifact, "", "  ")
	if err != nil {
		return "", fmt.Errorf("编码 frontend entry artifact 失败: %w", err)
	}
	artifactPath := filepath.Join(frontendRoot, entryArtifactFileName)
	if err := writeAtomic(artifactPath, append(encoded, '\n')); err != nil {
		return "", fmt.Errorf("写入 frontend entry artifact 失败: %w", err)
	}
	return artifactPath, nil
}

func digestFrontendAssets(outputDir string) (map[string]string, error) {
	assetsDir := filepath.Join(outputDir, "assets")
	files := map[string]string{}
	err := filepath.WalkDir(assetsDir, func(current string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("Vite assets 只能包含普通文件: %s", current)
		}
		relative, err := filepath.Rel(outputDir, current)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		digest, err := digestFrontendOutput(outputDir, relative)
		if err != nil {
			return err
		}
		files[relative] = digest
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历 Vite assets 失败: %w", err)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("Vite 输出缺少 assets 文件")
	}
	return files, nil
}

func singleViteEntry(entries map[string]viteManifestEntry) (string, viteManifestEntry, error) {
	keys := make([]string, 0)
	for key, entry := range entries {
		if entry.IsEntry {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) != 1 {
		return "", viteManifestEntry{}, fmt.Errorf("V1 frontend entry artifact 要求 Vite Manifest 恰好包含一个入口，当前为 %d", len(keys))
	}
	entry := entries[keys[0]]
	if !validFrontendOutputPath(entry.File) && entry.File != "frontend.mjs" {
		return "", viteManifestEntry{}, fmt.Errorf("Vite 入口必须是前端 root 或 assets/ 下的 ESM 文件: %q", entry.File)
	}
	return keys[0], entry, nil
}

func digestFrontendOutput(outputDir, relative string) (string, error) {
	if !validFrontendOutputPath(relative) {
		return "", fmt.Errorf("Vite 输出路径必须位于 assets/ 下: %q", relative)
	}
	path := filepath.Join(outputDir, filepath.FromSlash(relative))
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("读取 Vite 输出 %s 失败: %w", relative, err)
	}
	digest := sha256.Sum256(body)
	return "sha256-" + hex.EncodeToString(digest[:]), nil
}

func validFrontendOutputPath(value string) bool {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(value)))
	return strings.HasPrefix(value, "assets/") && clean == value && !strings.Contains(value, "\\")
}
