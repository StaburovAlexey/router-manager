package shell

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
)

type Runner interface {
	Run(ctx context.Context, name string, args ...string) error
	Output(ctx context.Context, name string, args ...string) (string, error)
}

type RealRunner struct{}

func (RealRunner) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (RealRunner) Output(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(out)), nil
}

type DryRunner struct {
	Calls   []string
	Outputs map[string]string
	Errors  map[string]error
}

func (r *DryRunner) Run(_ context.Context, name string, args ...string) error {
	key := commandKey(name, args...)
	r.Calls = append(r.Calls, key)
	if r.Errors != nil && r.Errors[key] != nil {
		return r.Errors[key]
	}
	return nil
}

func (r *DryRunner) Output(_ context.Context, name string, args ...string) (string, error) {
	key := commandKey(name, args...)
	r.Calls = append(r.Calls, key)
	if r.Errors != nil && r.Errors[key] != nil {
		return "", r.Errors[key]
	}
	if r.Outputs != nil {
		return r.Outputs[key], nil
	}
	return "", nil
}

func commandKey(name string, args ...string) string {
	if len(args) == 0 {
		return name
	}
	return name + " " + strings.Join(args, " ")
}
