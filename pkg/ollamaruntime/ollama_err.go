package ollamaruntime

import "errors"

func errNoEmbed() error { return errors.New("embedding not supported by bound provider") }
