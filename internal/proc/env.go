package proc

import (
	"os"
	"strings"
)

// SanitizedEnvironment returns the host environment with secret-bearing
// variables removed: API keys, tokens, passwords, credentials, cookies and
// cloud-provider credential namespaces never reach spawned processes.
//
// v1.1.4: moved here from internal/lab (it was duplicated in spirit by the
// sandbox, which passed the FULL os.Environ() to model-spawned code —
// python os.environ could read host API keys).
//
// overrides are applied last and force their value (e.g. HOME=workspace).
// Override keys are still checked against the denylist, so an override can
// never smuggle a secret back in.
func SanitizedEnvironment(overrides map[string]string) []string {
	base := os.Environ()
	result := make([]string, 0, len(base)+len(overrides))

	for _, item := range base {
		key := envKey(item)

		if key == "" || isSensitiveEnvKey(key) {
			continue
		}

		if _, forced := overrides[key]; forced {
			continue // the override replaces it below
		}

		result = append(result, item)
	}

	for key, value := range overrides {
		if key == "" || isSensitiveEnvKey(key) {
			continue
		}

		result = append(result, key+"="+value)
	}

	return result
}

func envKey(item string) string {
	i := strings.IndexByte(item, '=')
	if i <= 0 {
		return ""
	}
	return item[:i]
}

// IsSensitiveEnvKey reports whether an environment variable name is
// secret-bearing and must not enter spawned processes.
func IsSensitiveEnvKey(key string) bool { return isSensitiveEnvKey(key) }

func isSensitiveEnvKey(key string) bool {
	key = strings.ToUpper(strings.TrimSpace(key))

	if key == "" {
		return true
	}

	// Explicit high-value secret variables.
	switch key {
	case "OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"GEMINI_API_KEY",
		"GOOGLE_API_KEY",
		"GITHUB_TOKEN",
		"GH_TOKEN",
		"AWS_ACCESS_KEY_ID",
		"AWS_SECRET_ACCESS_KEY",
		"AWS_SESSION_TOKEN",
		"AZURE_CLIENT_SECRET",
		"NPM_TOKEN",
		"PYPI_TOKEN":
		return true
	}

	// Generic secret-bearing names.
	sensitiveFragments := []string{
		"API_KEY",
		"APIKEY",
		"ACCESS_TOKEN",
		"AUTH_TOKEN",
		"BEARER_TOKEN",
		"CLIENT_SECRET",
		"PASSWORD",
		"PASSWD",
		"SECRET",
		"TOKEN",
		"CREDENTIAL",
		"PRIVATE_KEY",
		"COOKIE",
		"SESSION_SECRET",
	}

	for _, fragment := range sensitiveFragments {
		if strings.Contains(key, fragment) {
			return true
		}
	}

	// Cloud/provider credential namespaces should not enter autonomous jobs.
	for _, prefix := range []string{
		"AWS_",
		"AZURE_",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"GCP_",
		"DOCKER_AUTH",
	} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}

	return false
}
