package support

import "testing"

func TestSMTPFromEnvironment(t *testing.T) {
	for _, tt := range []struct {
		name     string
		changes  map[string]string
		disabled bool
		invalid  bool
	}{
		{
			name:     "disabled",
			disabled: true,
		},
		{
			name: "STARTTLS defaults",
		},
		{
			name: "implicit TLS",
			changes: map[string]string{
				"SUPPORT_SMTP_PORT":     "465",
				"SUPPORT_SMTP_SECURITY": "tls",
			},
		},
		{
			name:    "missing credentials",
			changes: map[string]string{"SUPPORT_SMTP_PASSWORD": ""},
			invalid: true,
		},
		{
			name:    "plaintext forbidden",
			changes: map[string]string{"SUPPORT_SMTP_SECURITY": "none"},
			invalid: true,
		},
		{
			name:    "invalid port",
			changes: map[string]string{"SUPPORT_SMTP_PORT": "abc"},
			invalid: true,
		},
		{
			name: "header injection",
			changes: map[string]string{
				"SUPPORT_SMTP_FROM": "a@example.com\r\nBcc: b@example.com",
			},
			invalid: true,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{
				"SUPPORT_SMTP_HOST":     "smtp.example.com",
				"SUPPORT_SMTP_USERNAME": "user",
				"SUPPORT_SMTP_PASSWORD": "secret",
				"SUPPORT_SMTP_FROM":     "support@example.com",
			}

			if tt.disabled {
				clear(values)
			}

			for key, value := range tt.changes {
				values[key] = value
			}

			sender, err := SMTPFromEnvironment(func(key string) string {
				return values[key]
			})

			if (err != nil) != tt.invalid {
				t.Fatalf("error = %v", err)
			}

			if (sender == nil) != (tt.disabled || tt.invalid) {
				t.Fatalf("sender enabled = %v", sender != nil)
			}
		})
	}
}
