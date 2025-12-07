package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/linux/kernel"
)

func main() {
	fs := flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	arch := fs.String("arch", "x86_64", "Target architecture")
	extractKernel := fs.String("extract-kernel", "", "Extract the kernel for the specified architecture and save it to the given file")
	dumpConfigs := fs.String("dump-configs", "", "Dump the kernel configuration for the specified architecture to the given file")
	dumpDepends := fs.String("dump-depends", "", "Dump the kernel module dependency map for the specified architecture to the given file")

	if err := fs.Parse(os.Args[1:]); err != nil {
		os.Exit(1)
	}

	kernel, err := kernel.LoadForArchitecture(hv.CpuArchitecture(*arch))
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load kernel for architecture %s: %v\n", *arch, err)
		os.Exit(1)
	}

	if *extractKernel != "" {
		r, err := kernel.Open()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get kernel image: %v\n", err)
			os.Exit(1)
		}

		outFile, err := os.Create(*extractKernel)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create output file %q: %v\n", *extractKernel, err)
			os.Exit(1)
		}
		defer outFile.Close()

		if _, err := io.Copy(outFile, r); err != nil {
			fmt.Fprintf(os.Stderr, "failed to write kernel image to %q: %v\n", *extractKernel, err)
			os.Exit(1)
		}

		fmt.Printf("Extracted kernel for architecture %s to %q\n", *arch, *extractKernel)
	}

	if *dumpConfigs != "" {
		configs, err := kernel.GetConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get kernel config: %v\n", err)
			os.Exit(1)
		}

		outFile, err := os.Create(*dumpConfigs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create output file %q: %v\n", *dumpConfigs, err)
			os.Exit(1)
		}
		defer outFile.Close()

		for key, value := range configs {
			if _, err := fmt.Fprintf(outFile, "%s=%s\n", key, value); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write config to %q: %v\n", *dumpConfigs, err)
				os.Exit(1)
			}
		}

		fmt.Printf("Dumped kernel config for architecture %s to %q\n", *arch, *dumpConfigs)
	}

	if *dumpDepends != "" {
		depends, err := kernel.GetDependMap()
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to get kernel module dependency map: %v\n", err)
			os.Exit(1)
		}

		outFile, err := os.Create(*dumpDepends)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to create output file %q: %v\n", *dumpDepends, err)
			os.Exit(1)
		}
		defer outFile.Close()

		for mod, deps := range depends {
			if _, err := fmt.Fprintf(outFile, "%s: %v\n", mod, deps); err != nil {
				fmt.Fprintf(os.Stderr, "failed to write dependency map to %q: %v\n", *dumpDepends, err)
				os.Exit(1)
			}
		}

		fmt.Printf("Dumped kernel module dependency map for architecture %s to %q\n", *arch, *dumpDepends)
	}
}
