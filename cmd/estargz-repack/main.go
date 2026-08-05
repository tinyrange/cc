package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"j5.nz/cc/internal/oci"
)

func main() {
	var chunkSize int
	var minChunkSize int
	flag.IntVar(&chunkSize, "chunk-size", 256<<10, "uncompressed bytes per large-file chunk")
	flag.IntVar(&minChunkSize, "min-chunk-size", 256<<10, "minimum bytes grouped into one gzip member")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] INPUT-LAYER OUTPUT-LAYER\n", os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 2 {
		flag.Usage()
		os.Exit(2)
	}
	result, err := oci.RepackStargzLayerFile(context.Background(), flag.Arg(0), flag.Arg(1), chunkSize, minChunkSize)
	if err != nil {
		fmt.Fprintf(os.Stderr, "estargz-repack: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("blob=%s\nsize=%d\ndiff_id=%s\ntoc=%s\nmembers=%d\n",
		result.BlobDigest,
		result.BlobSize,
		result.DiffID,
		result.TOCJSONDigest,
		result.Members,
	)
}
