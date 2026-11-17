package origin

import "testing"

func TestAnotherOriginKeepsTheContractsPath(t *testing.T) {
	got, err := Server("https://api.onplo.com/v1", "http://127.0.0.1:8080")
	if err != nil || got != "http://127.0.0.1:8080/v1" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestPlainHTTPIsRefusedAwayFromThisMachine(t *testing.T) {
	if _, err := Server("https://api.onplo.com/v1", "http://api.example.test"); err == nil {
		t.Fatal("a key would have been sent over plain http")
	}
}
