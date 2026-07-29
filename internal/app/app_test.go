package app

import "testing"

func TestProfileArgument(t *testing.T) {
	tests := []struct {
		input []string
		want  string
		ok    bool
	}{
		{[]string{"--work-vpn"}, "work-vpn", true},
		{[]string{"personal"}, "personal", true},
		{[]string{}, "", false},
		{[]string{"--"}, "", false},
		{[]string{"--one", "--two"}, "", false},
	}
	for _, test := range tests {
		got, err := profileArgument(test.input)
		if (err == nil) != test.ok || got != test.want {
			t.Fatalf("profileArgument(%v) = %q, %v", test.input, got, err)
		}
	}
}

func TestParseProfileAddAcceptsDocumentedOrder(t *testing.T) {
	name, config, err := parseProfileAdd([]string{"work-vpn", "--config", `C:\VPN\client.ovpn`})
	if err != nil {
		t.Fatal(err)
	}
	if name != "work-vpn" || config != `C:\VPN\client.ovpn` {
		t.Fatalf("parseProfileAdd() = %q, %q", name, config)
	}
}
