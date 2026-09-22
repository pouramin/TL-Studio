package main

import (
	"context"
	"errors"
	"os"
	"strings"
)

type pluginActionDescriptor struct {
	ID                   string `json:"id"`
	Label                string `json:"label"`
	Kind                 string `json:"kind"`
	Target               string `json:"target,omitempty"`
	RequiresConfirmation bool   `json:"requiresConfirmation,omitempty"`
	Confirmation         string `json:"confirmation,omitempty"`
}

type pluginIntegrationView struct {
	ID      string                   `json:"id"`
	Status  string                   `json:"status,omitempty"`
	Summary string                   `json:"summary,omitempty"`
	Details map[string]string        `json:"details,omitempty"`
	Actions []pluginActionDescriptor `json:"actions,omitempty"`
}

type pluginIntegration interface {
	Matches(pluginConfig) bool
	Snapshot(pluginConfig, string) *pluginIntegrationView
	ValidateStart(pluginConfig, string) error
	RunAction(context.Context, *pluginManager, pluginConfig, string, string) (any, error)
}

func pluginIntegrations() []pluginIntegration {
	return []pluginIntegration{
		graphifyPluginIntegration{},
	}
}

func pluginIntegrationFor(config pluginConfig) pluginIntegration {
	for _, integration := range pluginIntegrations() {
		if integration.Matches(config) {
			return integration
		}
	}
	return nil
}

func pluginIntegrationSnapshot(config pluginConfig, project string) *pluginIntegrationView {
	integration := pluginIntegrationFor(config)
	if integration == nil {
		return nil
	}
	return integration.Snapshot(config, project)
}

func validatePluginIntegrationStart(config pluginConfig, project string) error {
	integration := pluginIntegrationFor(config)
	if integration == nil {
		return nil
	}
	return integration.ValidateStart(config, project)
}

func findPluginAction(view *pluginIntegrationView, actionID string) (pluginActionDescriptor, bool) {
	if view == nil {
		return pluginActionDescriptor{}, false
	}
	actionID = strings.TrimSpace(actionID)
	for _, action := range view.Actions {
		if action.ID == actionID {
			return action, true
		}
	}
	return pluginActionDescriptor{}, false
}

func (m *pluginManager) RunIntegrationAction(ctx context.Context, project, pluginID, actionID string, confirmed bool) (map[string]any, error) {
	config, found, err := m.store.find(project, pluginID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, os.ErrNotExist
	}
	integration := pluginIntegrationFor(config)
	if integration == nil {
		return nil, errors.New("plugin has no integration actions")
	}
	before := integration.Snapshot(config, project)
	action, ok := findPluginAction(before, actionID)
	if !ok {
		return nil, errors.New("plugin action not found")
	}
	if action.RequiresConfirmation && !confirmed {
		return nil, errors.New("plugin action requires explicit confirmation")
	}
	output, err := integration.RunAction(ctx, m, config, project, action.ID)
	view := m.viewConfig(project, config, config.Enabled)
	if err != nil {
		return map[string]any{"plugin": view, "result": output}, err
	}
	return map[string]any{"plugin": view, "result": output}, nil
}
