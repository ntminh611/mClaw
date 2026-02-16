// MClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 MClaw contributors

package main

import (
	"fmt"
	"os"

	"github.com/ntminh611/mclaw/cmd/mclaw/commands"
)

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "setup", "onboard":
		commands.RunSetup()
	case "agent":
		commands.RunAgent()
	case "start", "gateway":
		commands.RunStart()
	case "status":
		commands.RunStatus()
	case "cron":
		commands.RunCron()
	case "skills":
		commands.RunSkills()
	case "version", "--version", "-v":
		fmt.Printf("%s mclaw v%s\n", commands.Logo, commands.Version)
	default:
		fmt.Printf("Unknown command: %s\n", command)
		printHelp()
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Printf("%s mclaw - Personal AI Assistant v%s\n\n", commands.Logo, commands.Version)
	fmt.Println("Usage: mclaw <command>")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  start       Start mclaw server (all channels + cron + heartbeat)")
	fmt.Println("  agent       Interact with the agent directly")
	fmt.Println("  status      Show mclaw status")
	fmt.Println("  cron        Manage scheduled tasks")
	fmt.Println("  skills      Manage skills (install, list, remove)")
	fmt.Println("  version     Show version information")
}
