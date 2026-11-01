package harness

import (
	"fmt"
)

const (
	// containerCodexHome is codex's CODEX_HOME inside the sandbox, where the
	// CLI caches its ChatGPT OAuth login (auth.json) and reads global skills
	// (<containerCodexHome>/skills/<name>). The image pre-creates it
	// world-writable so the per-project auth volume mounts there writable.
	containerCodexHome = containerHome + "/.codex"
	// codexLoginPort is codex's OAuth callback port: the CLI's browser login
	// (codex login, or the TUI's sign-in screen) starts a loopback server
	// there and the authorization redirect goes to
	// http://localhost:1455/auth/callback, so the host side of the forward
	// must publish exactly that port. The relay (see sandbox.go) listens one
	// port up on the container's network, since the callback server itself
	// binds the container's loopback, which the proxy's forwarder can't dial.
	codexLoginPort = 1455
)

// codexExtraPolicy lists the OpenAI endpoints codex reaches directly, marked
// no-inspect so TLS is tunnelled unread: model responses stream over HTTP/2,
// and the OAuth host carries the login redirect and token refresh.
const codexExtraPolicy = `# OpenAI Codex (OAuth login, ChatGPT subscription backend, API)
no-inspect auth.openai.com:443
no-inspect api.openai.com:443
no-inspect chatgpt.com:443
no-inspect ab.chatgpt.com:443
`

func newCodex(opts Options) (Harness, error) {
	if !opts.Sandbox {
		return nil, fmt.Errorf("-harness codex requires -sandbox")
	}
	if err := checkExtraPolicy(opts.Policy, codexProfile()); err != nil {
		return nil, err
	}
	return newSandboxWithProfile(opts, codexProfile()), nil
}

func codexProfile() sandboxProfile {
	return sandboxProfile{
		harness:             "codex",
		needsModelServer:    false,
		usesGeneratedConfig: false,
		extraPolicy:         codexExtraPolicy,
		authVolume:          codexAuthVolume,
		authLabel:           "rnes.auth=codex",
		authMountTargets:    []string{containerCodexHome},
		skillsConfigHome:    containerCodexHome,
		skillsRelDir:        "skills",
		authCallbackPort:    codexLoginPort,
		containerArgs: func(modelID string, modelSet bool, extraArgs []string) []string {
			var out []string
			if modelSet && modelID != "" {
				out = append(out, "--model", modelID)
			}
			return append(out, extraArgs...)
		},
	}
}

// codexAuthVolume returns the deterministic per-project Docker named volume
// holding codex's OAuth credentials (auth.json) for dir, so the browser login
// persists across the project's sessions. Scoping by the canonical project
// path prevents cross-project credential leakage.
func codexAuthVolume(dir string) string {
	return projectAuthVolume("codex", dir)
}
