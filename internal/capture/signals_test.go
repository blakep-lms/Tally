package capture

import "testing"

func TestDomain(t *testing.T) {
	cases := map[string]string{
		"https://www.github.com/blakep-lms/tally": "github.com",
		"http://localhost:5600/api":               "localhost",
		"mail.google.com/mail/u/0":                "mail.google.com",
		"":                                        "",
		"not a url with spaces":                   "",
	}
	for in, want := range cases {
		if got := Domain(in); got != want {
			t.Errorf("Domain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRepo(t *testing.T) {
	cases := map[string]string{
		"tally — main":                       "tally",
		"~/code/tally/internal/store — nvim": "tally",
		"README.md":                          "",
		"just some window title":             "",
		"secureai-backend: feature/login":    "secureai-backend",
	}
	for in, want := range cases {
		if got := Repo(in); got != want {
			t.Errorf("Repo(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppBase(t *testing.T) {
	if got := AppBase("Google Chrome.app"); got != "Google Chrome" {
		t.Errorf("AppBase = %q", got)
	}
}
