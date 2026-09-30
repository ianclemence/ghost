// Preview tooling: seed a scratch Ghost directory so the web console can be
// run and inspected against synthetic data. Never touches the installed Ghost.
//
//	go run ./scripts/preview/seed /tmp/ghost-preview
package main

import (
	"os"
	"path/filepath"

	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/config"
)

func main() {
	dir := os.Args[1]
	for _, d := range []string{"config", "data", "workspace"} {
		_ = os.MkdirAll(filepath.Join(dir, d), 0o700)
	}
	cfg := config.DefaultConfig()
	cfg.Gateway.Port = 8392 // scripts/preview/mockpod.ts
	cfg.Agents.Defaults.Provider = "deepseek"
	cfg.Agents.Defaults.Model = "deepseek-flash"
	cfg.Providers.DeepSeek.APIKey = "sk-preview"
	if err := config.SaveConfig(filepath.Join(dir, "config", "config.json"), cfg); err != nil {
		panic(err)
	}
	if err := appliance.SetAdminPassword(dir, "preview-pass-1"); err != nil {
		panic(err)
	}
	_ = os.WriteFile(filepath.Join(dir, appliance.SetupCompleteFlag), []byte("1"), 0o600)
}
