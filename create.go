package main

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/cli/cli/v2/pkg/iostreams"
)

const separator = ", "

func (a *CreateArgs) GetHash() (string, error) {
	if len(a.Commitish) > 0 {
		return revParse(a.Commitish)
	}
	return revParse("HEAD")
}

func (a *CreateArgs) resolveMessage() error {
	if len(a.MessageFile) == 0 {
		return nil
	}

	if len(a.Message) > 0 {
		return fmt.Errorf("-m and -F are mutually exclusive")
	}

	if a.MessageFile == "-" && a.Edit {
		return fmt.Errorf("-F - cannot be combined with -e")
	}

	message, err := readMessageFile(a.MessageFile)
	if err != nil {
		return err
	}

	if len(message) == 0 && !a.Edit {
		return fmt.Errorf("empty revision comment in '%s': aborted", a.MessageFile)
	}

	a.Message = message

	return nil
}

func checkPrState(pr PullRequest) error {
	if pr.IsDraft {
		return fmt.Errorf("pr is draft")
	}
	if pr.State != "OPEN" {
		return fmt.Errorf("pr is not open")
	}
	if len(pr.Commits) == 0 {
		return fmt.Errorf("pr has no commits")
	}
	return nil
}

func checkGitState(pr PullRequest, hash string) error {
	if pr.Commits[len(pr.Commits)-1].Oid != hash {
		return fmt.Errorf("%s is not pr tip", hash)
	}
	return nil
}

func checkRevisionsState(revisions []Revision, hash string) error {
	for _, v := range revisions {
		if v.Hash == hash {
			return fmt.Errorf("%s already has revision %d", hash, v.Number)
		}
	}
	return nil
}

func newRevision(hash string, pr PullRequest, revisions []Revision, body string) (revision Revision, err error) {
	baseHash, err := revParse(fmt.Sprintf("%s^", pr.Commits[0].Oid))
	if err != nil {
		return revision, err
	}
	var number uint64 = 1
	if len(revisions) > 0 {
		number = revisions[len(revisions)-1].Number + 1
	}

	return Revision{Number: number, Hash: hash, BaseHash: baseHash, Comment: body}, nil
}

func link(url string, from, to int, base, hash string) string {
	return fmt.Sprintf("[%d..%d](%s/compare/%s..%s)", from, to, url, base, hash)
}

func linkFromRevisions(url string, from, to Revision) string {
	return link(url, int(from.Number), int(to.Number), from.Hash, to.Hash)
}

func linkLines(newRevision Revision, revisions []Revision) (links []string, err error) {
	repo, err := getRepository()
	if err != nil {
		return nil, err
	}

	revisions = append(revisions, newRevision)

	if len(revisions) == 1 {
		links = append(links, link(repo.Url, 0, 1, newRevision.BaseHash, newRevision.Hash))
		return links, nil
	}

	last := len(revisions) - 1
	for i := len(revisions) - 2; i >= 0; i -= 1 {
		next := i + 1
		var line []string
		line = append(line, linkFromRevisions(repo.Url, revisions[i], revisions[next]))
		if next != last {
			line = append(line, linkFromRevisions(repo.Url, revisions[i], revisions[last]))
		}
		links = append(links, strings.Join(line, separator))
	}

	var line []string
	line = append(line, link(repo.Url, 0, int(revisions[0].Number), revisions[0].BaseHash, revisions[0].Hash))
	line = append(line, link(repo.Url, 0, int(revisions[last].Number), revisions[0].BaseHash, revisions[last].Hash))
	links = append(links, strings.Join(line, separator))

	return links, nil
}

func reviewersLine(revision Revision) string {
	var reviewers []string
	for _, login := range revision.UserReviewers {
		reviewers = append(reviewers, fmt.Sprintf("@%s", login))
	}
	for _, slug := range revision.TeamReviewers {
		reviewers = append(reviewers, fmt.Sprintf("@%s", slug))
	}
	return strings.Join(reviewers, " ")
}

func newPrComment(args CreateArgs, newRevision Revision, revisions []Revision) (path string, err error) {
	links, err := linkLines(newRevision, revisions)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	fmt.Fprintf(&buf, "Revision %d\n", newRevision.Number)
	fmt.Fprintf(&buf, "---\n")

	if len(newRevision.Comment) > 0 {
		fmt.Fprintf(&buf, "%s", encodeBody(newRevision.Comment))
	}

	fmt.Fprintf(&buf, "**Compare**\n")
	for _, l := range links {
		fmt.Fprintf(&buf, "- %s\n", l)
	}
	fmt.Fprintf(&buf, "\n")

	if !args.NoReview {
		reviewers := reviewersLine(newRevision)
		if len(reviewers) > 0 {
			fmt.Fprintf(&buf, "**CC** %s\n", reviewers)
		}
		fmt.Fprintf(&buf, "\n")
	}

	fmt.Fprintf(&buf, "\n")

	encoded, err := encodeRevision(newRevision)
	if err != nil {
		return "", fmt.Errorf("failed to encode revision: %v", err)
	}
	fmt.Fprintf(&buf, "%s", encoded)

	tmpFile, err := os.CreateTemp("", "gh-pr-revision.*.md")
	if err != nil {
		return "", fmt.Errorf("failed to create temporary file: %v", err)
	}
	defer func() {
		_ = tmpFile.Close()
		if err != nil {
			_ = os.Remove(tmpFile.Name())
		}
	}()

	_, err = tmpFile.Write(buf.Bytes())
	if err != nil {
		return "", fmt.Errorf("failed to write comment file: %v", err)
	}

	return tmpFile.Name(), nil
}

func createRevision(args CreateArgs) error {
	ioStreams := iostreams.System()
	ioStreams.StartProgressIndicator()
	defer ioStreams.StopProgressIndicator()

	if err := args.resolveMessage(); err != nil {
		return err
	}

	hash, err := args.GetHash()
	if err != nil {
		return nil
	}

	pr, err := getPullRequest()
	if err != nil {
		return err
	}

	if err = checkPrState(pr); err != nil {
		return err
	}

	if err = checkGitState(pr, hash); err != nil {
		return err
	}

	revisions, err := parseRevisions(pr)
	if err != nil {
		return err
	}

	if err = checkRevisionsState(revisions, hash); err != nil {
		return err
	}

	var body string
	if args.Edit {
		ioStreams.StopProgressIndicator()
		if body, err = editUserComment(ioStreams, args.Message); err != nil {
			return err
		}
		ioStreams.StartProgressIndicator()
		if len(body) == 0 {
			return fmt.Errorf("empty revision comment: aborted")
		}
	} else {
		body = args.Message
	}

	newRev, err := newRevision(hash, pr, revisions, body)
	if err != nil {
		return err
	}

	apiPr, err := getPullRequestFromApi(pr.Owner.Login, pr.Repository.Name, pr.Number)
	if err != nil {
		return err
	}

	newRev.ExtendReviewers(revisions...)
	newRev.ExtendReviewersFromApi(apiPr)
	newRev.ExtendUserReviewers(args.UserReviewer...)
	newRev.ExtendTeamReviewers(args.TeamReviewer...)

	path, err := newPrComment(args, newRev, revisions)
	if err != nil {
		return err
	}
	defer os.Remove(path)

	out, err := addPrComment(path)
	if err != nil {
		return err
	}

	if !args.NoReview {
		if err := requestReviews(pr.Owner.Login, pr.Repository.Name, pr.Number, newRev); err != nil {
			return err
		}
	}

	ioStreams.StopProgressIndicator()

	fmt.Printf("Revision %d\n", newRev.Number)
	fmt.Printf("%s\n", out)

	return nil
}
