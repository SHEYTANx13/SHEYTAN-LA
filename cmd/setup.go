package cmd

import (
	"fmt"
	"os"
	"runtime"

	"github.com/Parsaetak/SHEYTAN-local-agent/internal/config"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/installer"
	"github.com/Parsaetak/SHEYTAN-local-agent/internal/sysinfo"
)

// v1.1.4: the file formerly known as diagnostics.go held Install /
// Sysinfo / Doctor / Setup — it is now named for what it actually contains.

// Install runs the component auto-installer + update check.
func Install(cfg *config.Config) int {
	mgr := installer.New(cfg)
	state, changes, err := mgr.EnsureRun(true)
	if err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		return 1
	}
	fmt.Println(installer.FormatState(state))
	if len(changes) > 0 {
		fmt.Println("\nChanges since last run:")
		for _, c := range changes {
			switch c.Kind {
			case "added":
				fmt.Printf("  + %s: %s\n", c.Component, c.To)
			case "removed":
				fmt.Printf("  - %s: was %s\n", c.Component, c.From)
			case "changed":
				fmt.Printf("  ~ %s: %s -> %s\n", c.Component, c.From, c.To)
			}
		}
	}
	return 0
}

// Sysinfo prints a detailed host probe.
func Sysinfo(cfg *config.Config) int {
	info := sysinfo.Probe()
	fmt.Printf("%s v%s — system info\n\n", config.AppName, config.AppVersion)
	fmt.Printf("OS:        %s/%s\n", info.OS, info.Arch)
	fmt.Printf("Hostname:  %s\n", info.Hostname)
	fmt.Printf("CPU:       %s (%d cores / %d threads)\n",
		info.CPU.Name, info.CPU.PhysicalCores, info.CPU.LogicalCores)
	fmt.Printf("RAM:       total=%s free=%s available=%s\n",
		sysinfo.FormatBytes(info.RAM.TotalBytes),
		sysinfo.FormatBytes(info.RAM.FreeBytes),
		sysinfo.FormatBytes(info.RAM.Available))
	if len(info.GPU) > 0 {
		for _, g := range info.GPU {
			vram := "—"
			if g.VRAMBytes > 0 {
				vram = sysinfo.FormatBytes(g.VRAMBytes)
			}
			fmt.Printf("GPU:       %s %s (VRAM=%s, driver=%s)\n", g.Vendor, g.Name, vram, g.DriverVer)
		}
	} else {
		fmt.Println("GPU:       none detected")
	}
	fmt.Printf("Disk:      total=%s free=%s (%s)\n",
		sysinfo.FormatBytes(info.Disk.TotalBytes),
		sysinfo.FormatBytes(info.Disk.FreeBytes),
		info.Disk.Path)
	fmt.Printf("WSL2:      %v\n", info.WSL2)
	fmt.Printf("Docker:    %v\n", info.Docker)
	fmt.Println()
	fmt.Println("Recommended llama.cpp knobs:")
	r := info.Recommended
	fmt.Printf("  num_thread=%d num_gpu=%d num_ctx=%d num_batch=%d max_tokens=%d\n",
		r.NumThread, r.NumGPU, r.NumCtx, r.NumBatch, r.MaxTokens)
	fmt.Printf("  can_run_cpu=%v can_run_gpu=%v\n", r.CanRunCPU, r.CanRunGPU)
	for _, w := range r.Warnings {
		fmt.Printf("  ⚠ %s\n", w)
	}
	return 0
}

// Doctor runs a full health check.
func Doctor(cfg *config.Config) int {
	info := sysinfo.Probe()
	fmt.Printf("%s v%s doctor — health check\n\n", config.AppName, config.AppVersion)
	pass, fail := 0, 0
	check := func(name string, ok bool, msg string) {
		mark := "✓"
		if !ok {
			mark = "✗"
			fail++
		} else {
			pass++
		}
		fmt.Printf("  %s %-15s %s\n", mark, name, msg)
	}
	check("go-runtime", true, runtime.Version())
	check("cpu", info.CPU.LogicalCores > 0, info.CPU.Name)
	check("ram", info.RAM.TotalBytes >= 4*1024*1024*1024, sysinfo.FormatBytes(info.RAM.TotalBytes))
	check("disk", info.Disk.FreeBytes > 1*1024*1024*1024, sysinfo.FormatBytes(info.Disk.FreeBytes))
	check("gpu", len(info.GPU) > 0, fmt.Sprintf("%d GPU(s)", len(info.GPU)))
	check("wsl2", info.WSL2, fmt.Sprintf("%v", info.WSL2))
	check("docker", info.Docker, fmt.Sprintf("%v", info.Docker))

	mgr := installer.New(cfg)
	state, _, _ := mgr.EnsureRun(false)
	if state != nil {
		for name, c := range state.Components {
			check(name, c.Status == "installed", c.Status)
		}
	}

	fmt.Printf("\n%d pass / %d fail\n", pass, fail)
	if fail > 0 {
		return 1
	}
	return 0
}

// Setup runs the first-run CLI wizard.
func Setup(cfg *config.Config) int {
	fmt.Printf("%s v%s — setup wizard\n\n", config.AppName, config.AppVersion)
	if err := cfg.EnsureDirs(); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		return 1
	}
	if err := config.Save(cfg.ConfigPath(), cfg); err != nil {
		fmt.Fprintln(os.Stderr, "save config:", err)
		return 1
	}
	fmt.Println("Configuration saved to:", cfg.ConfigPath())
	fmt.Println("Data directory:        ", cfg.DataDir)
	fmt.Println("Models directory:      ", cfg.ModelsDir)
	fmt.Println("Sessions directory:    ", cfg.SessionsDir)
	fmt.Println()
	fmt.Println("Next step: place a .gguf file in the models directory, then run:")
	fmt.Println("  sheytan-local-agent")
	return 0
}

// Stress runs the stress test suite.
func Stress(cfg *config.Config) int {
	fmt.Printf("%s v%s stress test — chaos suite\n\n",
		config.AppName, config.AppVersion)
	return runStressSuite(cfg)
}
