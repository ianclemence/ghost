package personality

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Personality struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Content     string `json:"content"`
	Builtin     bool   `json:"builtin,omitempty"`
}

type Loader struct {
	globalDir string
	builtin   map[string]*Personality
	active    string
}

func NewLoader(globalDir string) *Loader {
	l := &Loader{
		globalDir: filepath.Join(globalDir, "personalities"),
		builtin:   make(map[string]*Personality),
		active:    "default",
	}
	l.registerBuiltins()
	return l
}

func (l *Loader) registerBuiltins() {
	l.builtin["default"] = &Personality{
		Name:        "default",
		Description: "Standard Ghost personality — professional, concise, helpful",
		Builtin: true,
		Content: `You are Ghost, a personal AI assistant. Be professional, concise, and helpful.
Focus on accuracy and actionable information. Avoid unnecessary filler.`,
	}
	l.builtin["hacker"] = &Personality{
		Name:        "hacker",
		Description: "Technical deep-dive mode — thorough, precise, code-first",
		Builtin: true,
		Content: `You are Ghost in hacker mode. Be extremely technical and precise.
Provide code examples, reference documentation, and implementation details.
Skip non-technical explanations. Assume the user is a senior engineer.`,
	}
	l.builtin["creative"] = &Personality{
		Name:        "creative",
		Description: "Creative and expressive — brainstorming, writing, ideation",
		Builtin: true,
		Content: `You are Ghost in creative mode. Be expressive, imaginative, and exploratory.
Suggest unexpected angles, challenge assumptions, and offer alternatives.
Use vivid language and concrete examples.`,
	}
	l.builtin["teacher"] = &Personality{
		Name:        "teacher",
		Description: "Patient educator — explains concepts step by step",
		Builtin: true,
		Content: `You are Ghost in teacher mode. Be patient, clear, and thorough.
Explain concepts from first principles. Use analogies and examples.
Check understanding before moving to advanced topics.`,
	}
	l.builtin["minimal"] = &Personality{
		Name:        "minimal",
		Description: "Ultra-concise — one-line answers, no fluff",
		Builtin: true,
		Content: `You are Ghost in minimal mode. Answer in as few words as possible.
No explanations unless explicitly asked. One-line answers preferred.`,
	}
}

func (l *Loader) List() []*Personality {
	var all []*Personality

	for _, p := range l.builtin {
		all = append(all, p)
	}

	if err := os.MkdirAll(l.globalDir, 0755); err == nil {
		entries, err := os.ReadDir(l.globalDir)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() {
					continue
				}
				if !strings.HasSuffix(entry.Name(), ".json") {
					continue
				}
				name := strings.TrimSuffix(entry.Name(), ".json")
				if _, exists := l.builtin[name]; exists {
					continue
				}
				data, err := os.ReadFile(filepath.Join(l.globalDir, entry.Name()))
				if err != nil {
					continue
				}
				var p Personality
				if json.Unmarshal(data, &p) == nil && p.Name != "" {
					all = append(all, &p)
				}
			}
		}
	}

	sort.Slice(all, func(i, j int) bool {
		if all[i].Builtin != all[j].Builtin {
			return all[i].Builtin
		}
		return all[i].Name < all[j].Name
	})
	return all
}

func (l *Loader) Get(name string) (*Personality, bool) {
	name = strings.ToLower(strings.TrimSpace(name))
	if p, ok := l.builtin[name]; ok {
		return p, true
	}

	if err := os.MkdirAll(l.globalDir, 0755); err != nil {
		return nil, false
	}
	data, err := os.ReadFile(filepath.Join(l.globalDir, name+".json"))
	if err != nil {
		return nil, false
	}
	var p Personality
	if json.Unmarshal(data, &p) != nil {
		return nil, false
	}
	return &p, true
}

func (l *Loader) Set(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, ok := l.Get(name); !ok {
		return fmt.Errorf("personality %q not found", name)
	}
	l.active = name
	return nil
}

func (l *Loader) Active() string {
	return l.active
}

func (l *Loader) GetActiveContent() string {
	p, ok := l.Get(l.active)
	if !ok {
		return ""
	}
	return p.Content
}

func (l *Loader) Save(name, description, content string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, exists := l.builtin[name]; exists {
		return fmt.Errorf("cannot overwrite builtin personality %q", name)
	}
	if err := os.MkdirAll(l.globalDir, 0755); err != nil {
		return err
	}
	p := Personality{
		Name:        name,
		Description: description,
		Content:     content,
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(l.globalDir, name+".json"), data, 0644)
}

func (l *Loader) Delete(name string) error {
	name = strings.ToLower(strings.TrimSpace(name))
	if _, exists := l.builtin[name]; exists {
		return fmt.Errorf("cannot delete builtin personality %q", name)
	}
	path := filepath.Join(l.globalDir, name+".json")
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("personality %q not found", name)
	}
	if l.active == name {
		l.active = "default"
	}
	return nil
}
