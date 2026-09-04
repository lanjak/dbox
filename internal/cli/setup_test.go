package cli

import (
	"github.com/rufi/dbox/internal/config"
	"strings"
	"testing"
)

func TestSetupSavesPrivateConfigurationWithoutPrintingKey(t *testing.T) {
	p := t.TempDir() + "/dbox/config.json"
	var out strings.Builder
	c, err := Setup(strings.NewReader("test-key\n"+t.TempDir()+"\npro\n"), &out, p)
	if err != nil {
		t.Fatal(err)
	}
	if c.APIKey != "test-key" || c.Plan != config.Pro {
		t.Fatalf("bad config %#v", c)
	}
	if strings.Contains(out.String(), "test-key") {
		t.Fatal("setup printed API key")
	}
	if _, err := config.Load(p); err != nil {
		t.Fatal(err)
	}
}
