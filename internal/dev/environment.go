package dev

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

func pluginEnvironment(plan Plan) []string {
	prefix := envPrefix(plan.PluginID)
	env := []string{
		"SPARQ_APP_URL=" + plan.AppEndpoint,
		"SPARQ_PLUGIN_ID=" + plan.PluginID,
		prefix + "_APP_URL=" + plan.AppEndpoint,
		prefix + "_BASE_URL=" + plan.PluginEndpoint,
		prefix + "_PLUGIN_SECRET=" + plan.PluginSecret,
	}
	if plan.PluginComponent != "" {
		env = append(env, prefix+"_COMPONENT_ID="+plan.PluginComponent)
	}
	if plan.LifecycleSessionID != "" {
		env = append(env, "SPARQ_DEV_LIFECYCLE_SESSION_ID="+plan.LifecycleSessionID)
	}
	if plan.LifecycleSourceDigest != "" {
		env = append(env, "SPARQ_DEV_LIFECYCLE_SOURCE_DIGEST="+plan.LifecycleSourceDigest)
	}
	if address := endpointAddress(plan.PluginEndpoint); address != "" {
		env = append(env, prefix+"_ADDR="+address)
	}
	env = append(env, mapEnvironment(plan.PluginEnv)...)
	return env
}

func frontendEnvironment(plan Plan, token string) []string {
	env := []string{
		"VITE_SPARQ_HOST=" + plan.AppEndpoint,
		"VITE_SPARQ_PLUGIN_ID=" + plan.PluginID,
		"VITE_SPARQ_DEV_TOKEN=" + token,
	}
	return append(env, mapEnvironment(plan.FrontendEnv)...)
}

func mapEnvironment(values map[string]string) []string {
	output := make([]string, 0, len(values))
	for key, value := range values {
		output = append(output, key+"="+value)
	}
	return output
}

func envPrefix(pluginID string) string {
	var builder strings.Builder
	builder.WriteString("SPARQ_")
	for _, value := range strings.ToUpper(pluginID) {
		if (value >= 'A' && value <= 'Z') || (value >= '0' && value <= '9') {
			builder.WriteRune(value)
		} else {
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func endpointAddress(endpoint string) string {
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Host
}

func httpEndpointURL(endpoint, path string) (string, error) {
	target, err := endpointURL(endpoint, path)
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(target)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("地址必须使用 HTTP(S): %s", endpoint)
	}
	return target, nil
}

func endpointURL(endpoint, path string) (string, error) {
	parsed, err := url.Parse(strings.TrimRight(endpoint, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("地址无效: %s", endpoint)
	}
	if path == "" {
		return parsed.String(), nil
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/" + strings.TrimLeft(path, "/")
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func resolveOptionalPath(baseDir, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return resolvePath(baseDir, path)
}

func resolvePath(baseDir, path string) string {
	if strings.TrimSpace(path) == "" {
		return baseDir
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(baseDir, path)
}

func cloneEnv(values map[string]string) map[string]string {
	if len(values) == 0 {
		return map[string]string{}
	}
	output := make(map[string]string, len(values))
	for key, value := range values {
		output[key] = value
	}
	return output
}

func cloneStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append(make([]string, 0, len(values)), values...)
}

func defaultStrings(values, fallback []string) []string {
	if values == nil {
		return cloneStrings(fallback)
	}
	return values
}

func normalizeExtensions(values []string) []string {
	output := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		output = append(output, value)
	}
	return output
}

func resolveReloadPaths(pluginCWD string, values []string) []string {
	values = defaultStrings(values, []string{"."})
	output := make([]string, 0, len(values))
	for _, value := range values {
		output = append(output, filepath.Clean(resolvePath(pluginCWD, value)))
	}
	return output
}

func defaultString(value string, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
