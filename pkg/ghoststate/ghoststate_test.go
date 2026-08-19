package ghoststate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/db"
)

const testPassphrase = "correct horse battery staple"

func testWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	id, err := EnsureIdentity(ws)
	if err != nil {
		t.Fatalf("EnsureIdentity: %v", err)
	}
	_ = id

	d, err := db.NewDB(ws)
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	_, err = d.Exec(`INSERT INTO sessions (id, summary) VALUES ('sess-1', 'portable conversations must survive')`)
	if err != nil {
		t.Fatalf("insert session: %v", err)
	}
	_, err = d.Exec(`INSERT INTO messages (id, session_id, role, content) VALUES ('m-1', 'sess-1', 'user', 'remember to keep this memory')`)
	if err != nil {
		t.Fatalf("insert message: %v", err)
	}
	_, err = d.Exec(`INSERT INTO memory_chunks (id, content, embedding, source) VALUES ('c-1', 'the same Ghost must persist', '[]', 'conversation')`)
	if err != nil {
		t.Fatalf("insert memory chunk: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close db: %v", err)
	}

	files := map[string]string{
		"memory/MEMORY.md":              "# Memory\nKey fact: portability.",
		"knowledge/self/identity.md":    "# Identity\nI am a persistent Ghost.",
		"skills/meeting-notes/SKILL.md": "# meeting-notes\nA user-installed skill.",
		"cron/jobs.json":                `[{"name":"daily-summary"}]`,
		"USER.md":                       "# User\nPreferences go here.",
		"state/state.json":              `{"last_channel":"telegram"}`,
	}
	for rel, content := range files {
		path := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}
	return ws
}

func testConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Temperature = 0.42
	cfg.Providers.Moonshot.APIKey = "sk-test-secret"
	path := filepath.Join(dir, "config.json")
	if err := config.SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	return dir
}

func TestExportImportRoundTrip(t *testing.T) {
	ws := testWorkspace(t)
	cfgDir := testConfigDir(t)

	sourceID, err := EnsureIdentity(ws)
	if err != nil {
		t.Fatalf("source identity: %v", err)
	}

	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	manifest, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(cfgDir, "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if manifest.GhostID != sourceID.GhostID {
		t.Fatalf("manifest ghost_id %q != source %q", manifest.GhostID, sourceID.GhostID)
	}
	if manifest.SecretsIncluded {
		t.Fatal("secrets should not be included by default")
	}
	if manifest.File(configJSONLogical) == nil {
		t.Fatal("config not exported")
	}
	if manifest.File(conversationsFormatLogical) == nil {
		t.Fatal("conversations format marker not exported")
	}
	if len(manifest.Rebound) == 0 {
		t.Fatal("device-specific fields should be recorded as rebound")
	}
	reboundDB := false
	for _, r := range manifest.Rebound {
		if strings.Contains(r, "ghost.db") {
			reboundDB = true
		}
	}
	if !reboundDB {
		t.Fatalf("ghost.db should be recorded as rebound: %v", manifest.Rebound)
	}
	found := false
	for _, s := range manifest.SecretsExcluded {
		if s == configSecretsLogical {
			found = true
		}
	}
	if !found {
		t.Fatalf("config/.secrets.json not listed in SecretsExcluded: %v", manifest.SecretsExcluded)
	}

	// Fresh target: empty workspace and config dir.
	targetWS := t.TempDir()
	targetCfgDir := t.TempDir()
	targetCfgPath := filepath.Join(targetCfgDir, "config.json")

	imported, err := Import(ImportOptions{
		Workspace:  targetWS,
		ConfigPath: targetCfgPath,
		Source:     archive,
		Passphrase: testPassphrase,
	})
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if imported.GhostID != sourceID.GhostID {
		t.Fatalf("imported ghost_id %q != source %q", imported.GhostID, sourceID.GhostID)
	}

	// The same Ghost identity must be reconstructable on the new machine.
	targetID, err := LoadIdentity(targetWS)
	if err != nil {
		t.Fatalf("load target identity: %v", err)
	}
	if targetID.GhostID != sourceID.GhostID {
		t.Fatalf("target identity %q != source %q", targetID.GhostID, sourceID.GhostID)
	}

	// Portable state must match: the conversation survives and the runtime
	// database is rehydrated from the portable JSONL.
	d, err := db.NewDB(targetWS)
	if err != nil {
		t.Fatalf("open target db: %v", err)
	}
	defer d.Close()
	var msgCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM messages`).Scan(&msgCount); err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if msgCount != 1 {
		t.Fatalf("messages: got %d, want 1", msgCount)
	}
	var summary string
	if err := d.QueryRow(`SELECT summary FROM sessions WHERE id = 'sess-1'`).Scan(&summary); err != nil {
		t.Fatalf("read session summary: %v", err)
	}
	if summary != "portable conversations must survive" {
		t.Fatalf("session summary = %q, want the portable conversation to survive", summary)
	}
	// Embeddings are derived state: they are deliberately not portable and are
	// rebuilt as the agent runs on the new machine.
	var chunkCount int
	if err := d.QueryRow(`SELECT COUNT(*) FROM memory_chunks`).Scan(&chunkCount); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if chunkCount != 0 {
		t.Fatalf("memory_chunks: got %d, want 0 (embeddings are derived, not portable)", chunkCount)
	}

	for rel, want := range map[string]string{
		"memory/MEMORY.md":              "# Memory\nKey fact: portability.",
		"knowledge/self/identity.md":    "# Identity\nI am a persistent Ghost.",
		"skills/meeting-notes/SKILL.md": "# meeting-notes\nA user-installed skill.",
		"cron/jobs.json":                `[{"name":"daily-summary"}]`,
		"USER.md":                       "# User\nPreferences go here.",
	} {
		got, err := os.ReadFile(filepath.Join(targetWS, rel))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		if string(got) != want {
			t.Fatalf("%s: got %q, want %q", rel, got, want)
		}
	}

	// Configuration is restored, but device-specific values are rebound: the
	// workspace must point at the target, and the secret must NOT travel.
	targetCfg, err := config.LoadConfig(targetCfgPath)
	if err != nil {
		t.Fatalf("load imported config: %v", err)
	}
	if got, want := targetCfg.WorkspacePath(), targetWS; got != want {
		t.Fatalf("imported workspace %q, want target %q", got, want)
	}
	if got, want := targetCfg.Agents.Defaults.Temperature, 0.42; got != want {
		t.Fatalf("imported temperature %v, want %v", got, want)
	}
	if key := targetCfg.GetAPIKey(); key != "" {
		t.Fatalf("secret leaked into imported config: %q", key)
	}
	if fileExists(filepath.Join(targetCfgDir, ".secrets.json")) {
		t.Fatal("secrets file should not be restored when archive excludes secrets")
	}
}

func TestExportImportWithSecrets(t *testing.T) {
	ws := testWorkspace(t)
	cfgDir := testConfigDir(t)

	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	manifest, err := Export(ExportOptions{
		Workspace:      ws,
		ConfigPath:     filepath.Join(cfgDir, "config.json"),
		Destination:    archive,
		Passphrase:     testPassphrase,
		IncludeSecrets: true,
	})
	if err != nil {
		t.Fatalf("Export: %v", err)
	}
	if !manifest.SecretsIncluded {
		t.Fatal("secrets should be included when opted in")
	}
	if manifest.File(configSecretsLogical) == nil {
		t.Fatal("secrets file should be in archive when included")
	}

	targetWS := t.TempDir()
	targetCfgDir := t.TempDir()
	targetCfgPath := filepath.Join(targetCfgDir, "config.json")
	if _, err := Import(ImportOptions{
		Workspace:  targetWS,
		ConfigPath: targetCfgPath,
		Source:     archive,
		Passphrase: testPassphrase,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	secrets, err := config.LoadSecrets(filepath.Join(targetCfgDir, ".secrets.json"))
	if err != nil {
		t.Fatalf("load restored secrets: %v", err)
	}
	if got := secrets.ProviderAPIKeys["moonshot"]; got != "sk-test-secret" {
		t.Fatalf("restored moonshot key %q, want sk-test-secret", got)
	}
}

func TestExportFailsOnUnclassifiedArtifact(t *testing.T) {
	ws := testWorkspace(t)
	if err := os.WriteFile(filepath.Join(ws, "random.bin"), []byte("?"), 0644); err != nil {
		t.Fatalf("write random.bin: %v", err)
	}
	_, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(t.TempDir(), "config.json"),
		Destination: filepath.Join(t.TempDir(), "ghost.ghost"),
		Passphrase:  testPassphrase,
	})
	if err == nil {
		t.Fatal("export should fail on an unclassified artifact")
	}
	if !strings.Contains(err.Error(), "random.bin") {
		t.Fatalf("error should name the artifact, got: %v", err)
	}

	// Disposable artifacts (tmp/) are skipped silently by design.
	if err := os.Remove(filepath.Join(ws, "random.bin")); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(ws, "tmp"), 0755); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "tmp", "junk.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("write junk: %v", err)
	}
	m, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(t.TempDir(), "config.json"),
		Destination: filepath.Join(t.TempDir(), "ghost.ghost"),
		Passphrase:  testPassphrase,
	})
	if err != nil {
		t.Fatalf("export with disposable tmp should succeed: %v", err)
	}
	if m.File("tmp/junk.txt") != nil {
		t.Fatal("disposable artifact must not appear in manifest")
	}
}

func TestImportRefusesNonFresh(t *testing.T) {
	ws := testWorkspace(t)
	cfgDir := testConfigDir(t)
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	if _, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(cfgDir, "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// A target that already has identity state is not fresh.
	used := testWorkspace(t)
	_, err := Import(ImportOptions{
		Workspace:  used,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Source:     archive,
		Passphrase: testPassphrase,
	})
	if err == nil {
		t.Fatal("import into a used workspace must fail without --force")
	}
	if !strings.Contains(err.Error(), "not a fresh Ghost installation") {
		t.Fatalf("unexpected error: %v", err)
	}

	// --force overrides the guard.
	usedIDBefore, _ := LoadIdentity(used)
	if _, err := Import(ImportOptions{
		Workspace:  used,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Source:     archive,
		Passphrase: testPassphrase,
		Force:      true,
	}); err != nil {
		t.Fatalf("import with --force should succeed: %v", err)
	}
	usedIDAfter, _ := LoadIdentity(used)
	if usedIDBefore.GhostID == usedIDAfter.GhostID {
		t.Fatal("--force import should have replaced the target identity")
	}
}

func TestInspect(t *testing.T) {
	ws := testWorkspace(t)
	cfgDir := testConfigDir(t)
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	if _, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(cfgDir, "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	m, err := Inspect(archive, testPassphrase)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if m.Format != Format || m.SchemaVersion != CurrentSchemaVersion {
		t.Fatalf("bad manifest header: %+v", m)
	}
	if m.GhostID == "" {
		t.Fatal("inspect should reveal ghost_id without extracting")
	}

	if _, err := Inspect(archive, "wrong-passphrase"); err == nil {
		t.Fatal("inspect with wrong passphrase must fail")
	}
}

func TestWorkspaceArtifactClassification(t *testing.T) {
	portable := []string{
		"conversations/format.json",
		"conversations/sessions/a.jsonl",
		"state/identity.json",
		"sessions/2026.jsonl",
		"memory/MEMORY.md",
		"knowledge/self/identity.md",
		"skills/meeting-notes/SKILL.md",
		"cron/jobs.json",
		"USER.md", "AGENTS.md", "SOUL.md", "GHOST.md", "README.md",
		"kanban.json",
		".skills-sync.json",
	}
	rebound := []string{
		"ghost.db",
	}
	derived := []string{
		"state/state.json",
		"state/evolution/x.json",
		"skills/.bundled_manifest",
		"HEARTBEAT.md",
		"heartbeat.log",
		"generated/render.png",
		"media/tts.mp3",
		"media/delegation/spill.txt",
		"cache/images/a.png",
		"logs/subagent.log",
	}
	disposable := []string{
		"ghost.db-wal", "ghost.db-shm",
		"tmp/frames/1.png",
	}
	for _, p := range portable {
		cat, err := classifyWorkspaceFile(p)
		if err != nil || cat != CategoryPortable {
			t.Errorf("classifyWorkspaceFile(%q) = %q, %v; want portable", p, cat, err)
		}
	}
	for _, p := range rebound {
		cat, err := classifyWorkspaceFile(p)
		if err != nil || cat != CategoryRebound {
			t.Errorf("classifyWorkspaceFile(%q) = %q, %v; want rebound", p, cat, err)
		}
	}
	for _, p := range derived {
		cat, err := classifyWorkspaceFile(p)
		if err != nil || cat != CategoryDerived {
			t.Errorf("classifyWorkspaceFile(%q) = %q, %v; want derived", p, cat, err)
		}
	}
	for _, p := range disposable {
		cat, err := classifyWorkspaceFile(p)
		if err != nil || cat != CategoryDisposable {
			t.Errorf("classifyWorkspaceFile(%q) = %q, %v; want disposable", p, cat, err)
		}
	}
	if _, err := classifyWorkspaceFile("random.bin"); err == nil {
		t.Error("classifyWorkspaceFile(random.bin) should fail")
	}
}

func TestWorkspaceToolArtifactsRoundTrip(t *testing.T) {
	ws := testWorkspace(t)
	for _, rel := range []string{
		"kanban.json",
		".skills-sync.json",
		"generated/render.png",
		"media/tts.mp3",
		"media/delegation/spill.txt",
		"cache/images/a.png",
		"logs/subagent.log",
	} {
		p := filepath.Join(ws, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(p, []byte("tool-artifact:"+rel), 0644); err != nil {
			t.Fatalf("write %s: %v", rel, err)
		}
	}

	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	m, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  filepath.Join(t.TempDir(), "config.json"),
		Destination: archive,
		Passphrase:  testPassphrase,
	})
	if err != nil {
		t.Fatalf("Export with tool artifacts should succeed: %v", err)
	}
	if f := m.File("kanban.json"); f == nil || f.Category != CategoryPortable {
		t.Fatalf("kanban.json should be portable: %+v", f)
	}
	if f := m.File("generated/render.png"); f == nil || f.Category != CategoryDerived {
		t.Fatalf("generated/render.png should be derived: %+v", f)
	}
	if m.File("tmp/junk.txt") != nil {
		t.Fatal("disposable artifacts must not appear in manifest")
	}

	targetWS := t.TempDir()
	if _, err := Import(ImportOptions{
		Workspace:  targetWS,
		ConfigPath: filepath.Join(t.TempDir(), "config.json"),
		Source:     archive,
		Passphrase: testPassphrase,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}
	for _, rel := range []string{
		"kanban.json", ".skills-sync.json",
		"generated/render.png", "media/tts.mp3", "media/delegation/spill.txt",
		"cache/images/a.png", "logs/subagent.log",
	} {
		got, err := os.ReadFile(filepath.Join(targetWS, rel))
		if err != nil {
			t.Fatalf("read %s after import: %v", rel, err)
		}
		if want := "tool-artifact:" + rel; string(got) != want {
			t.Fatalf("%s: got %q, want %q", rel, got, want)
		}
	}
}

func TestModelConfigMigratesDeviceConfigRebounds(t *testing.T) {
	srcCfgDir := t.TempDir()
	srcCfg := config.DefaultConfig()
	srcCfg.Agents.Defaults.Workspace = "/device-a/workspace"
	srcCfg.Agents.Defaults.Provider = "openrouter"
	srcCfg.Agents.Defaults.Model = "some-model"
	srcCfg.Agents.Defaults.Temperature = 0.7
	srcCfg.Agents.Routing.LightModel = "light-model"
	srcCfg.Agents.ModelList = []config.ModelPreset{{Name: "fast", Provider: "groq", Model: "llama-3.1-8b"}}
	srcCfg.Providers.OpenRouter.APIBase = "http://device-a.local:8080/v1"
	srcCfg.Providers.Ollama.APIBase = "http://localhost:11434/v1"
	srcCfg.Providers.Groq.APIKey = "sk-source-secret"
	srcCfg.Gateway.Host = "192.168.1.50"
	srcCfg.Gateway.Port = 9000
	srcCfgPath := filepath.Join(srcCfgDir, "config.json")
	if err := config.SaveConfig(srcCfgPath, srcCfg); err != nil {
		t.Fatalf("save source config: %v", err)
	}

	ws := testWorkspace(t)
	archive := filepath.Join(t.TempDir(), "ghost.ghost")
	if _, err := Export(ExportOptions{
		Workspace:   ws,
		ConfigPath:  srcCfgPath,
		Destination: archive,
		Passphrase:  testPassphrase,
	}); err != nil {
		t.Fatalf("Export: %v", err)
	}

	// Target device has its own networking and its own secrets.
	tgtCfgDir := t.TempDir()
	tgtCfg := config.DefaultConfig()
	tgtCfg.Providers.OpenRouter.APIBase = "http://device-b.local:8080/v1"
	tgtCfg.Providers.Groq.APIKey = "sk-target-secret"
	tgtCfg.Gateway.Host = "10.0.0.5"
	tgtCfg.Gateway.Port = 8123
	tgtCfgPath := filepath.Join(tgtCfgDir, "config.json")
	if err := config.SaveConfig(tgtCfgPath, tgtCfg); err != nil {
		t.Fatalf("save target config: %v", err)
	}

	targetWS := t.TempDir()
	if _, err := Import(ImportOptions{
		Workspace:  targetWS,
		ConfigPath: tgtCfgPath,
		Source:     archive,
		Passphrase: testPassphrase,
	}); err != nil {
		t.Fatalf("Import: %v", err)
	}

	got, err := config.LoadConfig(tgtCfgPath)
	if err != nil {
		t.Fatalf("load imported config: %v", err)
	}

	// User preferences and model/router selections migrate.
	if got.Agents.Defaults.Provider != "openrouter" {
		t.Errorf("provider = %q, want openrouter", got.Agents.Defaults.Provider)
	}
	if got.Agents.Defaults.Model != "some-model" {
		t.Errorf("model = %q, want some-model", got.Agents.Defaults.Model)
	}
	if got.Agents.Defaults.Temperature != 0.7 {
		t.Errorf("temperature = %v, want 0.7", got.Agents.Defaults.Temperature)
	}
	if got.Agents.Routing.LightModel != "light-model" {
		t.Errorf("light_model = %q, want light-model", got.Agents.Routing.LightModel)
	}
	if len(got.Agents.ModelList) != 1 || got.Agents.ModelList[0].Name != "fast" {
		t.Errorf("model_list not migrated: %+v", got.Agents.ModelList)
	}

	// Device-specific configuration is preserved from the target, never
	// overwritten by the source's hardware.
	if got.Agents.Defaults.Workspace != targetWS {
		t.Errorf("workspace = %q, want target %q", got.Agents.Defaults.Workspace, targetWS)
	}
	if got.Gateway.Host != "10.0.0.5" || got.Gateway.Port != 8123 {
		t.Errorf("gateway = %s:%d, want target 10.0.0.5:8123", got.Gateway.Host, got.Gateway.Port)
	}
	if got.Providers.OpenRouter.APIBase != "http://device-b.local:8080/v1" {
		t.Errorf("openrouter api_base = %q, want target device-b", got.Providers.OpenRouter.APIBase)
	}
	if got.Providers.Ollama.APIBase != "" {
		t.Errorf("ollama api_base should stay unset on target, got %q", got.Providers.Ollama.APIBase)
	}

	// Secrets never cross boundaries: the target keeps its own, the source's
	// never arrives.
	secrets, err := config.LoadSecrets(filepath.Join(tgtCfgDir, ".secrets.json"))
	if err != nil {
		t.Fatalf("load target secrets: %v", err)
	}
	if got := secrets.ProviderAPIKeys["groq"]; got != "sk-target-secret" {
		t.Errorf("groq secret = %q, want target's own", got)
	}
	if got := secrets.ProviderAPIKeys["openai"]; got == "sk-source-secret" {
		t.Error("source secret leaked into target")
	}
	if key := got.GetAPIKey(); key == "sk-source-secret" {
		t.Error("source secret leaked into imported config")
	}
}

func TestValidateLogicalPath(t *testing.T) {
	for _, bad := range []string{"", "/etc/passwd", "../escape", "a/../../b", "..", ".", "./.."} {
		if err := validateLogicalPath(bad); err == nil {
			t.Errorf("validateLogicalPath(%q) should fail", bad)
		}
	}
	for _, good := range []string{"ghost.db", "memory/MEMORY.md", "config/config.json", "state/identity.json"} {
		if err := validateLogicalPath(good); err != nil {
			t.Errorf("validateLogicalPath(%q) should pass: %v", good, err)
		}
	}
}
