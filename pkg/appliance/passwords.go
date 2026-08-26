package appliance

import (
	"errors"
	"strings"
)

// commonPasswords is a set of the most frequently used passwords.
// Rejected to prevent users from choosing trivially guessable credentials.
var commonPasswords = map[string]bool{
	"password": true, "123456": true, "12345678": true, "qwerty": true,
	"abc123": true, "monkey": true, "1234567": true, "letmein": true,
	"trustno1": true, "dragon": true, "baseball": true, "iloveyou": true,
	"master": true, "sunshine": true, "ashley": true, "bailey": true,
	"passw0rd": true, "shadow": true, "123123": true, "654321": true,
	"superman": true, "qazwsx": true, "michael": true, "football": true,
	"password1": true, "password123": true, "admin": true, "admin123": true,
	"welcome": true, "welcome1": true, "hello": true, "charlie": true,
	"donald": true, "batman": true, "access": true, "whatever": true,
	"login": true, "starwars": true, "solo": true, "princess": true,
	"summer": true, "winter": true, "spring": true,
	"autumn": true, "master123": true, "passpass": true, "pass123": true,
	"pass1234": true, "pass12345": true, "pass123456": true, "password!": true,
	"password!1": true, "p@ssword": true, "p@ssw0rd": true, "P@ssw0rd": true,
	"Password": true, "Password1": true, "Password123": true, "Ghost": true,
	"ghost": true, "ghost123": true, "ghost1234": true, "changeme": true,
	"changeme123": true, "temp": true, "temp123": true, "test": true,
	"test123": true, "root": true, "root123": true, "toor": true,
	"pass": true, "guest": true, "guest123": true, "love": true,
	"god": true, "secret": true, "secret123": true, "hunter2": true,
	"killer": true, "qwerty123": true, "zapata": true, "shadow123": true,
	"matrix": true, "hacker": true, "hammer": true, "diamond": true,
	"silver": true, "golden": true, "master1": true, "monkey123": true,
	"dragon123": true, "access123": true, "mustang": true, "michael1": true,
	"ashley1": true, "jessica": true, "loveme": true, "fuckyou": true,
	"1234": true, "12345": true, "123456789": true, "1234567890": true,
	"000000": true, "111111": true, "1234567891": true, "abcdef": true,
	"abcdefg": true, "abc1234": true, "password12": true, "passwrd": true,
	"letmein123": true, "welcome123": true, "admin1": true, "admin12": true,
}

// ValidatePassword checks if a password meets minimum requirements.
// Returns an error if the password is too short or is a commonly used password.
func ValidatePassword(password string) error {
	if len(password) < 12 {
		return errors.New("password must be at least 12 characters — a longer passphrase is easier to remember and harder to guess")
	}
	lower := strings.ToLower(strings.TrimSpace(password))
	if commonPasswords[lower] {
		return errors.New("password is too common — choose something more unique")
	}
	return nil
}
