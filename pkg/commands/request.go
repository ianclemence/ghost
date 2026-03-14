package commands

type Request struct {
	Text       string
	Channel    string
	ChatID     string
	SessionKey string
	Reply      func(string) error
}
