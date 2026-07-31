package main

import (
	"flag"
	"fmt"
	"os"

	"j5.nz/cc/internal/virgl"
)

func main() {
	frame := flag.Int("frame", 0, "scanout checkpoint to render (zero selects the final frame)")
	resource := flag.Uint("resource", 0, "render this resource ID instead of the scanout resource")
	level := flag.Uint("level", 0, "render this mip level of the selected resource")
	draw := flag.Int("draw", 0, "render immediately after this draw within the selected frame")
	traceResource := flag.Uint("trace-resource", 0, "report draw state using this texture resource")
	traceDraws := flag.Bool("trace-draws", false, "report every draw state in the selected frame")
	summary := flag.Bool("summary", false, "print capture protocol statistics without replaying")
	flag.Parse()
	if *summary {
		if flag.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: virgl-replay -summary CAPTURE")
			os.Exit(2)
		}
		if err := virgl.SummarizeCapture(flag.Arg(0), os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "usage: virgl-replay [-frame N] CAPTURE OUTPUT.png")
		os.Exit(2)
	}
	var rendered int
	var err error
	if *traceDraws {
		if *traceResource != 0 || *resource != 0 || *level != 0 || *draw != 0 {
			fmt.Fprintln(os.Stderr, "-trace-draws cannot be combined with resource or draw selection")
			os.Exit(2)
		}
		rendered, err = virgl.ReplayCaptureTraceDraws(flag.Arg(0), flag.Arg(1), *frame, os.Stdout)
	} else if *traceResource != 0 {
		if *resource != 0 || *level != 0 || *draw != 0 {
			fmt.Fprintln(os.Stderr, "-trace-resource cannot be combined with -resource, -level, or -draw")
			os.Exit(2)
		}
		rendered, err = virgl.ReplayCaptureTraceResource(flag.Arg(0), flag.Arg(1), *frame, uint32(*traceResource), os.Stdout)
	} else if *resource != 0 {
		if *draw != 0 {
			if *level != 0 {
				fmt.Fprintln(os.Stderr, "-draw and -level cannot be combined")
				os.Exit(2)
			}
			rendered, err = virgl.ReplayCaptureResourceDraw(flag.Arg(0), flag.Arg(1), *frame, uint32(*resource), *draw)
		} else {
			rendered, err = virgl.ReplayCaptureResourceLevel(flag.Arg(0), flag.Arg(1), *frame, uint32(*resource), uint32(*level))
		}
	} else {
		if *level != 0 || *draw != 0 {
			fmt.Fprintln(os.Stderr, "-level and -draw require -resource")
			os.Exit(2)
		}
		rendered, err = virgl.ReplayCapture(flag.Arg(0), flag.Arg(1), *frame)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("rendered VirGL checkpoint %d to %s\n", rendered, flag.Arg(1))
}
