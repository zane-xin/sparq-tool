package app

import (
	"archive/tar"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	protocolrelease "github.com/zane-xin/sparq-protocol/release"
	artifactsignature "github.com/zane-xin/sparq-tool/internal/artifact/signature"
)

func TestPackageCreatesOfficialRuntimeAndAdminArchives(t *testing.T) {
	root := t.TempDir()
	runtimes := testRuntimeInputs(t, root)
	updaters := testUpdaterInputs(t, root)
	privateKey, publicKey := testSigningKey(t, root)
	admin := filepath.Join(root, "admin")
	writeTestFile(t, admin, "index.html", "shell")
	writeTestFile(t, admin, "assets/app.js", "asset")
	writeTestFile(t, admin, "sparq-admin-manifest.json", `{"format":"sparq.admin.artifact.v1","version":"0.1.0"}`)
	changelog := writeTestFile(t, root, "CHANGELOG.md", "## 0.1.0\n- First release\n")
	result, err := Package(PackageOptions{
		OutputPath: filepath.Join(root, "release", "0.1.0"), Version: "v0.1.0",
		Runtimes: runtimes, Updaters: updaters, AdminPath: admin, ChangelogPath: changelog, PublicBaseURL: "https://sparq.example.com",
		SigningKeyID: "official-test", SigningKeyPath: privateKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	manifestBody, err := os.ReadFile(result.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest protocolrelease.UpdateManifest
	if err := json.Unmarshal(manifestBody, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Runtimes) != 6 || len(manifest.Updaters) != 6 || manifest.Runtimes[0].LatestVersion != "0.1.0" || manifest.Runtimes[0].SHA256 == "" || manifest.Admin.SHA256 == "" || !manifest.Runtimes[0].RequiresRestart {
		t.Fatalf("manifest = %+v", manifest)
	}
	keys := map[string]ed25519.PublicKey{"official-test": publicKey}
	if err := manifest.ValidateForExecution(); err != nil {
		t.Fatalf("ValidateForExecution() error = %v", err)
	}
	if err := protocolrelease.VerifyManifestSignature(manifest, keys); err != nil {
		t.Fatalf("VerifyManifestSignature() error = %v", err)
	}
	runtimeBody, err := os.ReadFile(result.Runtimes[0].Path)
	if err != nil {
		t.Fatal(err)
	}
	if err := protocolrelease.VerifyArtifactSignature(manifest.Runtimes[0].Artifact, runtimeBody, keys); err != nil {
		t.Fatalf("VerifyArtifactSignature() error = %v", err)
	}
	if names := archiveNames(t, result.Runtimes[0].Path); len(names) != 1 || names[0] != "sparq" {
		t.Fatalf("runtime archive = %#v", names)
	}
	if names := archiveNames(t, result.Updaters[0].Path); len(names) != 1 || names[0] != "sparq-updater" {
		t.Fatalf("updater archive = %#v", names)
	}
	names := archiveNames(t, result.AdminPath)
	if len(names) != 4 || names[0] != "assets/" || names[1] != "assets/app.js" || names[2] != "index.html" || names[3] != "sparq-admin-manifest.json" {
		t.Fatalf("admin archive = %#v", names)
	}
	if _, err := Package(PackageOptions{OutputPath: result.Directory, Version: "0.1.0", Runtimes: runtimes, Updaters: updaters, AdminPath: admin, ChangelogPath: changelog, PublicBaseURL: "https://sparq.example.com", SigningKeyID: "official-test", SigningKeyPath: privateKey}); err == nil {
		t.Fatal("expected existing release directory to be rejected")
	}
}

func TestPackageRejectsRuntimePluginAssets(t *testing.T) {
	root := t.TempDir()
	runtimes := testRuntimeInputs(t, root)
	updaters := testUpdaterInputs(t, root)
	privateKey, _ := testSigningKey(t, root)
	admin := filepath.Join(root, "admin")
	writeTestFile(t, admin, "index.html", "shell")
	writeTestFile(t, admin, "sparq-admin-manifest.json", `{"format":"sparq.admin.artifact.v1","version":"0.1.0"}`)
	writeTestFile(t, admin, "plugin-assets/cms/index.js", "runtime asset")
	changelog := writeTestFile(t, root, "CHANGELOG.md", "## 0.1.0\n")
	_, err := Package(PackageOptions{OutputPath: filepath.Join(root, "release"), Version: "0.1.0", Runtimes: runtimes, Updaters: updaters, AdminPath: admin, ChangelogPath: changelog, PublicBaseURL: "https://sparq.example.com", SigningKeyID: "official-test", SigningKeyPath: privateKey})
	if err == nil {
		t.Fatal("expected runtime plugin assets to be rejected")
	}
}

func testUpdaterInputs(t *testing.T, root string) []UpdaterInput {
	t.Helper()
	platforms := [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}}
	result := make([]UpdaterInput, 0, len(platforms))
	for _, platform := range platforms {
		name := "sparq-updater"
		if platform[0] == "windows" {
			name += ".exe"
		}
		path := writeTestFile(t, root, filepath.Join("updater", platform[0], platform[1], name), platform[0]+platform[1]+"-updater")
		result = append(result, UpdaterInput{TargetOS: platform[0], TargetArch: platform[1], BinaryPath: path})
	}
	return result
}

func testSigningKey(t *testing.T, root string) (string, ed25519.PublicKey) {
	t.Helper()
	pair, err := artifactsignature.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(root, "release-private.pem")
	if err := os.WriteFile(privatePath, pair.PrivatePEM, 0o600); err != nil {
		t.Fatal(err)
	}
	block, _ := pem.Decode(pair.PublicPEM)
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return privatePath, parsed.(ed25519.PublicKey)
}

func testRuntimeInputs(t *testing.T, root string) []RuntimeInput {
	t.Helper()
	platforms := [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}, {"windows", "amd64"}, {"windows", "arm64"}}
	result := make([]RuntimeInput, 0, len(platforms))
	for _, platform := range platforms {
		name := "sparq"
		if platform[0] == "windows" {
			name += ".exe"
		}
		path := writeTestFile(t, root, filepath.Join("bin", platform[0], platform[1], name), platform[0]+platform[1])
		result = append(result, RuntimeInput{TargetOS: platform[0], TargetArch: platform[1], BinaryPath: path})
	}
	return result
}

func archiveNames(t *testing.T, path string) []string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	gzipReader, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	defer gzipReader.Close()
	reader := tar.NewReader(gzipReader)
	var names []string
	for {
		header, err := reader.Next()
		if err != nil {
			break
		}
		names = append(names, header.Name)
	}
	return names
}

func writeTestFile(t *testing.T, root, relative, body string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}
