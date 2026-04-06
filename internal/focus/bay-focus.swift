#!/usr/bin/env swift
// bay-focus: macOS helper for switching Spaces and activating applications.
// Compiled by `bay setup` and called by bay's navigation commands.
//
// Usage:
//   bay-focus --app <bundle-id>      Activate app and switch to its Space
//   bay-focus --check                Check if Accessibility permission is granted
//
// Requirements:
//   - macOS Accessibility permission (System Settings → Privacy → Accessibility)
//   - Ctrl+1..9 keyboard shortcuts for Spaces enabled (System Settings → Keyboard → Shortcuts → Mission Control)
//
// Does NOT require: Screen Recording, SIP disabled, window title matching.

import AppKit
import CoreGraphics

// Private SPI for Space management.
@_silgen_name("CGSCopySpacesForWindows")
func CGSCopySpacesForWindows(_ connection: Int32, _ selector: Int32, _ windowIDs: CFArray) -> CFArray?

@_silgen_name("CGSGetActiveSpace")
func CGSGetActiveSpace(_ connection: Int32) -> Int

@_silgen_name("CGSMainConnectionID")
func CGSMainConnectionID() -> Int32

// Find which Space a window is on.
func spaceForWindow(_ windowID: CGWindowID) -> Int? {
    let conn = CGSMainConnectionID()
    let ids = [windowID] as CFArray
    guard let spaces = CGSCopySpacesForWindows(conn, 0x7, ids) as? [Int],
          let space = spaces.first else {
        return nil
    }
    return space
}

// Get the currently active Space index (1-based).
func activeSpaceIndex() -> Int? {
    let conn = CGSMainConnectionID()
    let activeSpace = CGSGetActiveSpace(conn)

    // Get all Spaces in order to find the index.
    guard let displays = CGSCopyManagedDisplaySpaces(conn) as? [[String: Any]] else {
        return nil
    }

    var index = 1
    for display in displays {
        guard let spaces = display["Spaces"] as? [[String: Any]] else { continue }
        for space in spaces {
            guard let spaceID = space["id64"] as? Int else { continue }
            if spaceID == activeSpace {
                return index
            }
            index += 1
        }
    }
    return nil
}

@_silgen_name("CGSCopyManagedDisplaySpaces")
func CGSCopyManagedDisplaySpaces(_ connection: Int32) -> CFArray?

// Find Space index for a given Space ID.
func indexForSpace(_ targetSpace: Int) -> Int? {
    let conn = CGSMainConnectionID()
    guard let displays = CGSCopyManagedDisplaySpaces(conn) as? [[String: Any]] else {
        return nil
    }

    var index = 1
    for display in displays {
        guard let spaces = display["Spaces"] as? [[String: Any]] else { continue }
        for space in spaces {
            guard let spaceID = space["id64"] as? Int else { continue }
            if spaceID == targetSpace {
                return index
            }
            index += 1
        }
    }
    return nil
}

// Switch to a Space by index using Ctrl+N keyboard shortcut simulation.
func switchToSpace(_ index: Int) {
    guard index >= 1 && index <= 9 else { return }

    // Key codes for 1-9
    let keyCodes: [Int: CGKeyCode] = [
        1: 0x12, 2: 0x13, 3: 0x14, 4: 0x15, 5: 0x17,
        6: 0x16, 7: 0x1A, 8: 0x1C, 9: 0x19
    ]

    guard let keyCode = keyCodes[index] else { return }

    let ctrlFlag = CGEventFlags.maskControl

    let keyDown = CGEvent(keyboardEventSource: nil, virtualKey: keyCode, keyDown: true)
    keyDown?.flags = ctrlFlag
    keyDown?.post(tap: .cghidEventTap)

    let keyUp = CGEvent(keyboardEventSource: nil, virtualKey: keyCode, keyDown: false)
    keyUp?.flags = ctrlFlag
    keyUp?.post(tap: .cghidEventTap)

    // Brief delay for Space transition.
    usleep(300_000)
}

// Activate an application by bundle ID.
func activateApp(_ bundleID: String) -> Bool {
    guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: bundleID).first else {
        return false
    }
    return app.activate(options: [.activateIgnoringOtherApps])
}

// Check Accessibility permission.
func checkAccessibility() -> Bool {
    let trusted = AXIsProcessTrustedWithOptions(
        [kAXTrustedCheckOptionPrompt: false] as CFDictionary
    )
    return trusted
}

// Main
let args = CommandLine.arguments

if args.contains("--check") {
    if checkAccessibility() {
        print("Accessibility: granted")
        exit(0)
    } else {
        print("Accessibility: not granted")
        print("Enable in: System Settings → Privacy & Security → Accessibility")
        exit(1)
    }
}

guard let appIndex = args.firstIndex(of: "--app"),
      appIndex + 1 < args.count else {
    fputs("Usage: bay-focus --app <bundle-id> | --check\n", stderr)
    exit(1)
}

let bundleID = args[appIndex + 1]

// Find the app's frontmost window and its Space.
guard let app = NSRunningApplication.runningApplications(withBundleIdentifier: bundleID).first else {
    fputs("App not running: \(bundleID)\n", stderr)
    exit(1)
}

// Get the app's windows.
let windowList = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID) as? [[String: Any]] ?? []

var targetWindowID: CGWindowID?
for window in windowList {
    guard let ownerPID = window[kCGWindowOwnerPID as String] as? pid_t,
          ownerPID == app.processIdentifier,
          let windowID = window[kCGWindowNumber as String] as? CGWindowID else {
        continue
    }
    targetWindowID = windowID
    break
}

if let windowID = targetWindowID,
   let space = spaceForWindow(windowID),
   let targetIndex = indexForSpace(space) {
    let conn = CGSMainConnectionID()
    let currentSpace = CGSGetActiveSpace(conn)
    if space != currentSpace {
        switchToSpace(targetIndex)
    }
}

// Activate the app.
if !activateApp(bundleID) {
    fputs("Failed to activate: \(bundleID)\n", stderr)
    exit(1)
}
