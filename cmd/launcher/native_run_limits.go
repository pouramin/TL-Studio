package main

import (
    "fmt"
    "time"
)

// Limits are per run. Zero means the documented default (or no optional token/idle cap).
// Hard upper bounds remain in place even when the UI sends a higher custom budget.
const (
    nativeAgentMaximumConfigurableToolRounds = 100
    nativeAgentDefaultToolRounds = 24
    nativeAgentMaximumRunTokens = 10000000
)

func validateNativeRunLimits(input sessionRunInput) error {
    if input.MaxToolRounds < 0 || input.MaxToolRounds > nativeAgentMaximumConfigurableToolRounds {
        return fmt.Errorf("maxToolRounds must be between 1 and %d, or 0 for default", nativeAgentMaximumConfigurableToolRounds)
    }
    if input.ModelTimeoutSeconds < 0 || (input.ModelTimeoutSeconds != 0 && (input.ModelTimeoutSeconds < 30 || input.ModelTimeoutSeconds > 1200)) {
        return fmt.Errorf("modelTimeoutSeconds must be between 30 and 1200, or 0 for default")
    }
    if input.StreamIdleTimeoutSeconds < 0 || (input.StreamIdleTimeoutSeconds != 0 && (input.StreamIdleTimeoutSeconds < 10 || input.StreamIdleTimeoutSeconds > 300)) {
        return fmt.Errorf("streamIdleTimeoutSeconds must be between 10 and 300, or 0 to disable")
    }
    if input.MaxRunTokens < 0 || input.MaxRunTokens > nativeAgentMaximumRunTokens {
        return fmt.Errorf("maxRunTokens must be between 1 and %d, or 0 to disable", nativeAgentMaximumRunTokens)
    }
    return nil
}

func nativeRunToolRoundLimit(input sessionRunInput) int {
    if input.MaxToolRounds > 0 && input.MaxToolRounds <= nativeAgentMaximumConfigurableToolRounds {
        return input.MaxToolRounds
    }
    return nativeAgentDefaultToolRounds
}

func nativeRunIterationLimit(input sessionRunInput) int {
    rounds := nativeRunToolRoundLimit(input)
    if rounds+8 > nativeAgentMaxIterations {
        return rounds+8
    }
    return nativeAgentMaxIterations
}

func nativeRunTurnTimeout(input sessionRunInput, fallback time.Duration) time.Duration {
    if input.ModelTimeoutSeconds >= 30 && input.ModelTimeoutSeconds <= 1200 {
        return time.Duration(input.ModelTimeoutSeconds) * time.Second
    }
    return fallback
}

func nativeRunIdleTimeout(input sessionRunInput) time.Duration {
    if input.StreamIdleTimeoutSeconds >= 10 && input.StreamIdleTimeoutSeconds <= 300 {
        return time.Duration(input.StreamIdleTimeoutSeconds) * time.Second
    }
    return 0
}

func nativeRunBillableTokens(usage sessionUsage) int64 {
    output := usage.Output
    if output == 0 && usage.Reasoning > 0 {
        output = usage.Reasoning
    }
    return usage.Input + output
}

func nativeRunBudgetReportPrompt(reason string) string {
    return "Execution safety budget reached (" + reason + "). Do not use or request any tools. " +
        "Give a concise factual checkpoint with: (1) what you actually completed and verified; " +
        "(2) what remains unfinished or unverified; and (3) the safest next action to resume. " +
        "Never claim that unfinished work is complete."
}
