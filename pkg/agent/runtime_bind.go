package agent

import (
	"github.com/ianclemence/ghost/pkg/infer"
	ollamaruntime "github.com/ianclemence/ghost/pkg/ollamaruntime"
)

// InferenceRuntime exposes the loop's current provider/model behind the
// runtime-independent infer.InferenceRuntime contract. Pod-local execution
// programs against capabilities here, never against Ollama specifics.
func (al *AgentLoop) InferenceRuntime() infer.InferenceRuntime {
	if al == nil {
		return nil
	}
	return ollamaruntime.New(al.provider, al.model)
}
