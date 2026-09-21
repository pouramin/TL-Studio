package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type nativePermissionWaiter struct {
	item     map[string]any
	response chan string
}

func (e *permissionEngine) setEventBus(bus *liveEventBus) {
	e.events = bus
}

func nativePermissionMatcher(descriptor toolDescriptor, input map[string]any) (string, bool) {
	switch descriptor.ID {
	case "files.write", "files.edit":
		path, _ := input["path"].(string)
		path = strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))
		if path == "" {
			return "", false
		}
		return descriptor.ID + ":" + path, true
	case "terminal.command":
		command, _ := input["command"].(string)
		command = strings.TrimSpace(command)
		if command == "" {
			return "", false
		}
		return descriptor.ID + ":" + command, true
	default:
		return descriptor.ID, descriptor.PermissionClass != "read"
	}
}

func nativePermissionPatterns(descriptor toolDescriptor, input map[string]any) []string {
	switch descriptor.ID {
	case "files.write", "files.edit", "files.read", "files.list":
		if path, _ := input["path"].(string); strings.TrimSpace(path) != "" {
			return []string{strings.TrimSpace(strings.ReplaceAll(path, "\\", "/"))}
		}
	case "terminal.command":
		if command, _ := input["command"].(string); strings.TrimSpace(command) != "" {
			return []string{strings.TrimSpace(command)}
		}
	}
	return nil
}

func (e *permissionEngine) AuthorizeNativeTool(
	ctx context.Context,
	sessionID, project string,
	descriptor toolDescriptor,
	input map[string]any,
) error {
	if descriptor.ID == "" || descriptor.ID == "runtime.unknown" {
		return errors.New("unknown tools cannot be authorized")
	}
	if descriptor.PermissionClass == "read" && !descriptor.Capabilities.Write && !descriptor.Capabilities.Execute && !descriptor.Capabilities.Network {
		return nil
	}

	matcher, canRemember := nativePermissionMatcher(descriptor, input)
	sensitive := descriptor.Capabilities.Execute || descriptor.Capabilities.Network
	if canRemember && !sensitive && e.store != nil {
		covered, err := e.store.covers(project, descriptor.PermissionClass, []string{matcher})
		if err != nil {
			return err
		}
		if covered {
			return nil
		}
	}

	requestID, err := randomSecret(12)
	if err != nil {
		return err
	}
	metadata := map[string]any{}
	if sensitive {
		metadata["disableAlways"] = true
		if descriptor.Capabilities.Execute {
			metadata["skillShell"] = true
		}
	}
	always := []string{}
	if canRemember && !sensitive {
		always = []string{matcher}
	}
	item := map[string]any{
		"id":         requestID,
		"sessionID":  strings.TrimSpace(sessionID),
		"permission": descriptor.PermissionClass,
		"action":     descriptor.Name,
		"patterns":   nativePermissionPatterns(descriptor, input),
		"always":     always,
		"metadata":   metadata,
		"toolID":     descriptor.ID,
		"source":     "tl-studio-native",
	}
	waiter := &nativePermissionWaiter{item: item, response: make(chan string, 1)}

	e.nativeMu.Lock()
	if e.nativePending == nil {
		e.nativePending = map[string]*nativePermissionWaiter{}
	}
	e.nativePending[requestID] = waiter
	e.nativeMu.Unlock()
	if e.events != nil {
		e.events.publish(liveEventView{
			Type: "attention.changed", Action: "requested",
			SessionID: sessionID, AttentionKind: "permission",
		})
	}

	defer func() {
		e.nativeMu.Lock()
		delete(e.nativePending, requestID)
		e.nativeMu.Unlock()
		if e.events != nil {
			e.events.publish(liveEventView{
				Type: "attention.changed", Action: "resolved",
				SessionID: sessionID, AttentionKind: "permission",
			})
		}
	}()

	select {
	case decision := <-waiter.response:
		if decision == "allow" {
			return nil
		}
		return errors.New("tool permission was rejected")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *permissionEngine) nativePendingSnapshot(sessionID string) []map[string]any {
	e.nativeMu.Lock()
	defer e.nativeMu.Unlock()
	result := []map[string]any{}
	for _, waiter := range e.nativePending {
		if sessionID != "" && permissionSessionID(waiter.item) != sessionID {
			continue
		}
		copyItem := make(map[string]any, len(waiter.item))
		for key, value := range waiter.item {
			copyItem[key] = value
		}
		result = append(result, copyItem)
	}
	return result
}

func (e *permissionEngine) replyNativePermission(
	requestID, sessionID, reply string,
) (map[string]any, bool, error) {
	e.nativeMu.Lock()
	waiter, ok := e.nativePending[requestID]
	e.nativeMu.Unlock()
	if !ok {
		return nil, false, nil
	}
	if sessionID != "" && permissionSessionID(waiter.item) != sessionID {
		return nil, true, errors.New("permission request does not belong to this session")
	}
	switch reply {
	case "once":
		select {
		case waiter.response <- "allow":
		default:
		}
		return map[string]any{"ok": true, "reply": "once", "remembered": 0}, true, nil
	case "always":
		if !permissionCanRemember(waiter.item) {
			return nil, true, errors.New("this permission cannot be remembered safely")
		}
		matchers := permissionAlwaysMatchers(waiter.item)
		added, err := e.store.addAllowRules(
			normalizePermissionProjectFromValue(waiter.item, e.state.projectPath()),
			permissionString(waiter.item),
			matchers,
		)
		if err != nil {
			return nil, true, err
		}
		select {
		case waiter.response <- "allow":
		default:
		}
		return map[string]any{"ok": true, "reply": "once", "remembered": len(added)}, true, nil
	case "reject":
		select {
		case waiter.response <- "reject":
		default:
		}
		return map[string]any{"ok": true, "reply": "reject", "remembered": 0}, true, nil
	default:
		return nil, true, fmt.Errorf("reply must be once, always, or reject")
	}
}

func normalizePermissionProjectFromValue(_ map[string]any, fallback string) string {
	return fallback
}
