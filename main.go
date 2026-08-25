package main

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/alexflint/go-arg"
)

//go:embed version.txt
var version string

type CreateArgs struct {
	Commitish    string   `arg:"-c, --commitish" help:"a commitish to associate the revision with (HEAD if omitted)"`
	Edit         bool     `arg:"-e, --editor" help:"open an editor to add revision comment"`
	Message      string   `arg:"-m, --message" help:"add this as revision comment"`
	MessageFile  string   `arg:"-F, --message-file" help:"read revision comment from this file ('-' for stdin)"`
	UserReviewer []string `arg:"-r, --reviewer,separate" help:"add a user reviewer to the pull-request"`
	TeamReviewer []string `arg:"-t, --team-reviewer,separate" help:"add a team reviewer to the pull-request"`
	NoReview     bool     `arg:"-n, --no-review" help:"do not send review requests"`
}

type DiffArgs struct {
	From uint64 `arg:"positional,required"`
	To   uint64 `arg:"positional,required"`
}

type ListArgs struct {
	Verbose bool `arg:"-v, --verbose" help:"enable verbose mode"`
}

type ShowArgs struct {
	Number uint64 `arg:"required,positional"`
}

type Args struct {
	Create   *CreateArgs `arg:"subcommand:create" help:"create revision"`
	Diff     *DiffArgs   `arg:"subcommand:diff" help:"compare revisions with 'git diff'"`
	DiffTool *DiffArgs   `arg:"subcommand:difftool" help:"compare revisions with 'git difftool'"`
	List     *ListArgs   `arg:"subcommand:list" help:"list revisions"`
	Show     *ShowArgs   `arg:"subcommand:show" help:"show revision"`
}

func (Args) Description() string {
	return "GitHub CLI extension for pull request revisions"
}

func (Args) Version() string {
	return version
}

func main() {
	var args Args

	p, err := arg.NewParser(arg.Config{}, &args)
	if err != nil {
		panic(fmt.Sprintf("bad Args definition: %v", err))
	}

	err = p.Parse(os.Args[1:])
	switch {
	case err == arg.ErrHelp: // found "--help" on command line
		_ = p.WriteHelpForSubcommand(os.Stdout, p.SubcommandNames()...)
		os.Exit(0)
	case err == arg.ErrVersion: // found "--version" on command line
		fmt.Println(args.Version())
		os.Exit(0)
	case err != nil:
		fmt.Fprintf(os.Stderr, "error: failed to parse args: %v\n", err)
		_ = p.WriteUsageForSubcommand(os.Stderr, p.SubcommandNames()...)
		os.Exit(1)
	}

	if p.Subcommand() == nil {
		p.WriteUsage(os.Stderr)
		os.Exit(1)
	}

	switch {
	case args.Create != nil:
		err = createRevision(*args.Create)
	case args.Diff != nil:
		err = diffRevisions(*args.Diff, "diff")
	case args.DiffTool != nil:
		err = diffRevisions(*args.DiffTool, "difftool")
	case args.List != nil:
		err = listRevisions(*args.List)
	case args.Show != nil:
		err = showRevision(*args.Show)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v", err)
		os.Exit(1)
	}
}
