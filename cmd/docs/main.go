package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

const (
	dirPerm = 0o755
)

func main() {
	var (
		projectRoot string
		targetDir   string
		configFile  string
	)

	flag.StringVar(&projectRoot, "project-root", ".", "Path to the project root")
	flag.StringVar(&targetDir, "target-dir", "assets", "Target directory for assets")
	flag.StringVar(&configFile, "config-file", "mkdocs.yml", "Path to the config file (relative to project root)")
	flag.Parse()

	absRoot, err := filepath.Abs(projectRoot)
	if err != nil {
		fmt.Printf("Error: failed to get absolute path for project root: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("Generating documentation for project at %s\n", absRoot)

	// 1. Detect build tool
	buildTool := detectBuildTool()
	switch buildTool {
	case "":
		fmt.Println("Warning: Neither zensical nor mkdocs found. Static documentation site will not be built.")
	case "mkdocs":
		fmt.Println("Recommendation: Use zensical for a faster and more integrated experience!")
	}

	ctx := context.Background()

	// 2. Sync raw docs for TUI
	err = syncRawDocs(absRoot, targetDir)
	if err != nil {
		fmt.Printf("Error: failed to sync raw docs: %v\n", err)
		os.Exit(1)
	}

	// 3. Build static site
	if buildTool != "" {
		err = buildStaticSite(ctx, absRoot, targetDir, configFile, buildTool)
		if err != nil {
			fmt.Printf("Error: failed to build static site: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Println("Documentation assets updated successfully.")
}

func detectBuildTool() string {
	if _, err := exec.LookPath("zensical"); err == nil {
		return "zensical"
	}

	if _, err := exec.LookPath("mkdocs"); err == nil {
		return "mkdocs"
	}

	return ""
}

func syncRawDocs(root, targetBase string) error {
	fmt.Println("Syncing raw documentation assets...")

	destDocs := filepath.Join(root, targetBase, "docs")

	err := os.RemoveAll(destDocs)
	if err != nil {
		return err
	}

	err = os.MkdirAll(destDocs, dirPerm)
	if err != nil {
		return err
	}

	// Copy docs folder
	srcDocs := filepath.Join(root, "docs")

	err = copyDir(srcDocs, destDocs)
	if err != nil {
		return err
	}

	// Copy config file if it exists
	srcConfig := filepath.Join(root, "mkdocs.yml")
	if _, err := os.Stat(srcConfig); err == nil {
		err = copyFile(srcConfig, filepath.Join(destDocs, "mkdocs.yml"))
		if err != nil {
			return err
		}
	}

	srcZensical := filepath.Join(root, "zensical.toml")
	if _, err := os.Stat(srcZensical); err == nil {
		err = copyFile(srcZensical, filepath.Join(destDocs, "zensical.toml"))
		if err != nil {
			return err
		}
	}

	return nil
}

func buildStaticSite(ctx context.Context, root, targetBase, configFile, tool string) error {
	fmt.Printf("Building documentation using %s...\n", tool)

	var cmd *exec.Cmd
	if tool == "zensical" {
		cmd = exec.CommandContext(ctx, "zensical", "build")
	} else {
		cmd = exec.CommandContext(ctx, "mkdocs", "build", "-f", configFile)
	}

	cmd.Dir = root
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return err
	}

	// Move site to assets
	srcSite := filepath.Join(root, "site")
	destSite := filepath.Join(root, targetBase, "site")

	err = os.RemoveAll(destSite)
	if err != nil {
		return err
	}

	// Ensure target directory exists
	err = os.MkdirAll(filepath.Dir(destSite), dirPerm)
	if err != nil {
		return err
	}

	return os.Rename(srcSite, destSite)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}

	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}

	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return out.Close()
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		targetPath := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(targetPath, info.Mode())
		}

		return copyFile(path, targetPath)
	})
}
