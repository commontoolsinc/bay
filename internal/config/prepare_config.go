package config

import (
	"fmt"
	"time"
)

const (
	PrepareRunAuto = "auto"
)

var validPrepareBlocks = map[string]bool{
	"agent": true,
	"cmd":   true,
}

// UnmarshalTOML records field presence so per-field prepare overrides can
// distinguish absent values from explicitly empty values.
func (c *BayPrepareConfig) UnmarshalTOML(data interface{}) error {
	raw, ok := data.(map[string]interface{})
	if !ok {
		return fmt.Errorf("bay_prepare entry must be a table")
	}
	for key, value := range raw {
		switch key {
		case "name":
			c.fields.Name = true
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("bay_prepare.name must be a string")
			}
			c.Name = s
		case "command":
			c.fields.Command = true
			values, err := stringSliceField("bay_prepare.command", value)
			if err != nil {
				return err
			}
			c.Command = values
		case "ready_command":
			c.fields.ReadyCommand = true
			values, err := stringSliceField("bay_prepare.ready_command", value)
			if err != nil {
				return err
			}
			c.ReadyCommand = values
		case "blocks":
			c.fields.Blocks = true
			values, err := stringSliceField("bay_prepare.blocks", value)
			if err != nil {
				return err
			}
			c.Blocks = values
		case "run":
			c.fields.Run = true
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("bay_prepare.run must be a string")
			}
			c.Run = s
		case "timeout":
			c.fields.Timeout = true
			s, ok := value.(string)
			if !ok {
				return fmt.Errorf("bay_prepare.timeout must be a string")
			}
			c.Timeout = s
		default:
			return fmt.Errorf("bay_prepare.%s is not a known field", key)
		}
	}
	return nil
}

func stringSliceField(field string, value interface{}) ([]string, error) {
	raw, ok := value.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", field)
	}
	values := make([]string, len(raw))
	for i, item := range raw {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", field, i)
		}
		values[i] = s
	}
	return values, nil
}

// ValidatePrepareSource validates one unmerged prepare source, including
// source-local uniqueness.
func ValidatePrepareSource(source string, steps []BayPrepareConfig) []string {
	var errs []string
	seen := map[string]bool{}
	for i, step := range steps {
		label := prepareStepLabel(source, step, i)
		if step.Name == "" {
			errs = append(errs, fmt.Sprintf("%s: name must be non-empty", label))
			continue
		}
		if seen[step.Name] {
			errs = append(errs, fmt.Sprintf("%s: duplicate name %q", source, step.Name))
			continue
		}
		seen[step.Name] = true
		if prepareCommandPresent(step) && len(step.Command) == 0 {
			errs = append(errs, fmt.Sprintf("%s: command must be a non-empty argv array when present", label))
		}
		if prepareReadyCommandPresent(step) && len(step.ReadyCommand) == 0 {
			errs = append(errs, fmt.Sprintf("%s: ready_command must be a non-empty argv array when present", label))
		}
		for _, block := range step.Blocks {
			if !validPrepareBlocks[block] {
				errs = append(errs, fmt.Sprintf("%s: blocks contains unknown surface class %q", label, block))
			}
		}
		if prepareRunPresent(step) && step.Run != PrepareRunAuto {
			errs = append(errs, fmt.Sprintf("%s: run must be %q", label, PrepareRunAuto))
		}
		if prepareTimeoutPresent(step) {
			d, err := time.ParseDuration(step.Timeout)
			if err != nil || d <= 0 {
				errs = append(errs, fmt.Sprintf("%s: timeout must be a positive duration", label))
			}
		}
	}
	return errs
}

// ValidatePrepare validates the effective, merged prepare config.
func ValidatePrepare(steps []BayPrepareConfig) []string {
	var errs []string
	seen := map[string]bool{}
	for i, step := range steps {
		label := prepareStepLabel("bay_prepare", step, i)
		if step.Name == "" {
			errs = append(errs, fmt.Sprintf("%s: name must be non-empty", label))
		} else if seen[step.Name] {
			errs = append(errs, fmt.Sprintf("bay_prepare: duplicate name %q", step.Name))
		}
		seen[step.Name] = true

		if len(step.Command) == 0 {
			errs = append(errs, fmt.Sprintf("%s: command must be a non-empty argv array", label))
		}
		if prepareReadyCommandPresent(step) && len(step.ReadyCommand) == 0 {
			errs = append(errs, fmt.Sprintf("%s: ready_command must be a non-empty argv array when present", label))
		}
		for _, block := range step.Blocks {
			if !validPrepareBlocks[block] {
				errs = append(errs, fmt.Sprintf("%s: blocks contains unknown surface class %q", label, block))
			}
		}
		if len(step.Blocks) > 0 && len(step.ReadyCommand) == 0 {
			errs = append(errs, fmt.Sprintf("%s: ready_command is required when blocks is non-empty", label))
		}
		if prepareRunPresent(step) && step.Run != PrepareRunAuto {
			errs = append(errs, fmt.Sprintf("%s: run must be %q", label, PrepareRunAuto))
		}
		if prepareTimeoutPresent(step) {
			d, err := time.ParseDuration(step.Timeout)
			if err != nil || d <= 0 {
				errs = append(errs, fmt.Sprintf("%s: timeout must be a positive duration", label))
			}
		}
	}
	return errs
}

func prepareStepLabel(source string, step BayPrepareConfig, index int) string {
	if step.Name != "" {
		return fmt.Sprintf("%s[%q]", source, step.Name)
	}
	return fmt.Sprintf("%s[%d]", source, index)
}

func prepareCommandPresent(step BayPrepareConfig) bool {
	return step.fields.Command || step.Command != nil
}

func prepareReadyCommandPresent(step BayPrepareConfig) bool {
	return step.fields.ReadyCommand || step.ReadyCommand != nil
}

func prepareBlocksPresent(step BayPrepareConfig) bool {
	return step.fields.Blocks || step.Blocks != nil
}

func prepareRunPresent(step BayPrepareConfig) bool {
	return step.fields.Run || step.Run != ""
}

func prepareTimeoutPresent(step BayPrepareConfig) bool {
	return step.fields.Timeout || step.Timeout != ""
}
