package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"

	"github.com/andyzysjtu/FileList/internal/scan"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("filelist", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", "", "directory or volume to scan (required)")
	base := flags.String("base", "", "fixed path base; defaults to the containing volume/mount root")
	sourceID := flags.String("source-id", "", "persistent data source ID; reuse it on later scans (required)")
	output := flags.String("output", "-", "new JSONL output file, or - for stdout")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: filelist --root PATH --source-id ID [--output FILE] [--base PATH]")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	if flags.NArg() != 0 || *root == "" || strings.TrimSpace(*sourceID) == "" || *output == "" {
		fmt.Fprintln(stderr, "filelist: --root and --source-id are required; use --help for usage")
		return 1
	}
	options := scan.Options{Root: *root, Base: *base, SourceID: *sourceID}
	writer := stdout
	var file *os.File
	if *output != "-" {
		var err error
		file, err = os.OpenFile(*output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
		if err != nil {
			fmt.Fprintln(stderr, "filelist: create output:", err)
			return 1
		}
		writer = file
	}
	buffer := bufio.NewWriterSize(writer, 64*1024)
	summary, scanErr := scan.Run(ctx, buffer, options)
	flushErr := buffer.Flush()
	var closeErr error
	if file != nil {
		closeErr = file.Close()
	}
	if err := errors.Join(scanErr, flushErr, closeErr); err != nil {
		fmt.Fprintln(stderr, "filelist:", err)
		if errors.Is(err, context.Canceled) && flushErr == nil && closeErr == nil {
			return 130
		}
		return 1
	}
	fmt.Fprintf(stderr, "Scanned %d entries (%d files, %d directories), %d errors, %d skipped\n",
		summary.Entries, summary.Files, summary.Directories, summary.Errors, summary.Skipped)
	if !summary.Complete {
		return 2
	}
	return 0
}
