package main

import (
	"os"
	"strconv"
	"strings"
)

// configureKiloRuntimeDefaults applies the defaults expected by the standalone
// browser client before the Kilo subprocess inherits the launcher's environment.
//
// These settings deliberately keep the bundled runtime local-first:
//   - PostHog and OpenTelemetry are disabled for TL-Studio-launched Kilo.
//   - the Question tool is enabled because this client implements its host UI.
//   - Kilo watches the launcher PID so a hard-killed launcher does not leave an
//     orphaned backend process behind.
//   - edits ask for permission by default unless the caller explicitly supplied
//     KILO_CONFIG_CONTENT. This mirrors the safety boundary used by Kilo's IDE
//     clients while still respecting an explicit caller override.
func configureKiloRuntimeDefaults() {
	_ = os.Setenv("KILO_TELEMETRY_LEVEL", "off")
	_ = os.Setenv("DO_NOT_TRACK", "1")
	_ = os.Setenv("OTEL_SDK_DISABLED", "true")
	_ = os.Setenv("KILO_ENABLE_QUESTION_TOOL", "true")
	_ = os.Setenv("KILO_PARENT_PID", strconv.Itoa(os.Getpid()))

	if strings.TrimSpace(os.Getenv("KILO_CONFIG_CONTENT")) == "" {
		_ = os.Setenv("KILO_CONFIG_CONTENT", `{"permission":{"edit":"ask"}}`)
	}
}

func init() {
	configureKiloRuntimeDefaults()
}
