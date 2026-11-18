package handoff

import (
	"fmt"
	"os/exec"

	"git.lanjak.net/rufi/dbox/internal/config"
)

func CommandFor(target config.HandoffTarget, override string) string {
	if override != "" {
		return override
	}
	if target == config.HandoffAccela {
		return "accela"
	}
	return ""
}

func Launch(target config.HandoffTarget, override, archive string) error {
	command := CommandFor(target, override)
	if command == "" {
		return nil
	}
	if _, err := exec.LookPath(command); err != nil && override == "" {
		return fmt.Errorf("%s launcher not found on PATH", command)
	}
	if err := exec.Command(command, archive).Start(); err != nil {
		return fmt.Errorf("launch %s: %w", command, err)
	}
	return nil
}
