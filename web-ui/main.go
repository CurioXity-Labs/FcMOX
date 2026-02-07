package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"fireadmin/handlers"
	"fireadmin/vm"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

func main() {
	var (
		port       int
		fcBin      string
		kernelPath string
		masterRoot string
		rootfsDir  string
		bootArgs   string
	)

	flag.IntVar(&port, "port", 8080, "HTTP listen port")
	flag.StringVar(&fcBin, "firecracker", "", "Path to firecracker binary (default: auto-detect)")
	flag.StringVar(&kernelPath, "kernel", "", "Path to kernel image (default: ../lk-images/vmlinux-6.12-ebpf)")
	flag.StringVar(&masterRoot, "rootfs", "", "Path to master rootfs image (default: ../lk-rootfs/rootfs.ext4)")
	flag.StringVar(&rootfsDir, "rootfs-dir", "", "Directory for VM rootfs copies (default: ../)")
	flag.StringVar(&bootArgs, "boot-args", "", "Kernel boot arguments")
	flag.Parse()

	// Resolve paths relative to the binary's parent (project root)
	baseDir := resolveBaseDir()

	if fcBin == "" {
		fcBin = filepath.Join(baseDir, "firecracker")
	}
	if kernelPath == "" {
		kernelPath = filepath.Join(baseDir, "lk-images", "vmlinux-6.12-ebpf")
	}
	if masterRoot == "" {
		masterRoot = filepath.Join(baseDir, "lk-rootfs", "rootfs.ext4")
	}
	if rootfsDir == "" {
		rootfsDir = baseDir
	}

	// Validate critical paths
	for _, check := range []struct{ name, path string }{
		{"firecracker binary", fcBin},
		{"kernel image", kernelPath},
		{"master rootfs", masterRoot},
	} {
		if _, err := os.Stat(check.path); os.IsNotExist(err) {
			log.Printf("⚠  %s not found: %s", check.name, check.path)
		}
	}

	cfg := vm.Config{
		FirecrackerBin: fcBin,
		KernelPath:     kernelPath,
		MasterRootFS:   masterRoot,
		RootFSDir:      rootfsDir,
		BootArgs:       bootArgs,
	}

	mgr := vm.NewManager(cfg)

	apiH := &handlers.APIHandler{Mgr: mgr}
	consoleH := &handlers.ConsoleHandler{Mgr: mgr}
	sshH := &handlers.SSHHandler{Mgr: mgr}

	// Echo
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
	}))

	// Static files
	e.Static("/", "static")

	// REST API
	api := e.Group("/api")
	api.GET("/vms", apiH.ListVMs)
	api.POST("/vms", apiH.CreateVM)
	api.GET("/vms/:id", apiH.GetVM)
	api.POST("/vms/:id/start", apiH.StartVM)
	api.POST("/vms/:id/stop", apiH.StopVM)
	api.DELETE("/vms/:id", apiH.DestroyVM)

	// WebSocket console (serial PTY mirror)
	e.GET("/ws/console/:id", consoleH.ServeConsole)

	// WebSocket SSH (independent sessions)
	e.GET("/ws/ssh/:id", sshH.ServeSSH)

	log.Printf("🔥 FireAdmin listening on :%d", port)
	log.Printf("   Firecracker: %s", fcBin)
	log.Printf("   Kernel:      %s", kernelPath)
	log.Printf("   Master RFS:  %s", masterRoot)
	log.Printf("   RootFS Dir:  %s", rootfsDir)

	e.Logger.Fatal(e.Start(fmt.Sprintf(":%d", port)))
}

// resolveBaseDir finds the project root (parent of web-ui/).
func resolveBaseDir() string {
	// Try relative to CWD first
	cwd, _ := os.Getwd()
	if filepath.Base(cwd) == "web-ui" {
		return filepath.Dir(cwd)
	}

	// Try relative to executable
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		if filepath.Base(dir) == "web-ui" {
			return filepath.Dir(dir)
		}
	}

	// Fallback: assume CWD is project root
	return cwd
}
