package objstore_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/runatlantis/atlantis/server/core/objstore"
	"github.com/runatlantis/atlantis/server/core/objstore/objstoretest"
	. "github.com/runatlantis/atlantis/testing"
)

func TestKeyJoinsUnderPrefix(t *testing.T) {
	b := objstore.NewBucket(objstoretest.New(), objstore.Config{Bucket: "b", Prefix: "/atlantis/plans/"})
	Equals(t, "atlantis/plans/org/repo/1/default/dir/sub/p.tfplan", b.Key("org", "repo", "1", "default", "dir/sub", "p.tfplan"))
	Equals(t, "atlantis/plans/org/repo/1", b.Key("org", "", "repo", "1"))
	Equals(t, "x", objstore.NewBucket(nil, objstore.Config{}).Key("x"))
}

func TestPutAppliesEncryption(t *testing.T) {
	fake := objstoretest.New()
	b := objstore.NewBucket(fake, objstore.Config{Bucket: "b", ServerSideEncryption: "aws:kms", KMSKeyID: "alias/atlantis"})
	Ok(t, b.Put(context.Background(), "k", strings.NewReader("x"), nil))
	Equals(t, "aws:kms", string(fake.Objects["k"].Encryption))
	Equals(t, "alias/atlantis", fake.Objects["k"].KMSKeyID)
}

func TestListFollowsPaginationAndDeletePrefix(t *testing.T) {
	fake := objstoretest.New()
	b := objstore.NewBucket(fake, objstore.Config{Bucket: "b"})
	for _, k := range []string{"p/1", "p/2", "p/3", "p/4", "p/5", "q/1"} {
		Ok(t, b.Put(context.Background(), k, strings.NewReader(k), nil))
	}
	var got []string
	for k, err := range b.List(context.Background(), "p/") {
		Ok(t, err)
		got = append(got, k)
	}
	Equals(t, []string{"p/1", "p/2", "p/3", "p/4", "p/5"}, got)

	n, err := b.DeletePrefix(context.Background(), "p/")
	Ok(t, err)
	Equals(t, 5, n)
	Equals(t, []string{"q/1"}, fake.Keys())
}

func TestDeletePrefixReportsFailures(t *testing.T) {
	fake := objstoretest.New()
	b := objstore.NewBucket(fake, objstore.Config{Bucket: "b"})
	Ok(t, b.Put(context.Background(), "p/1", strings.NewReader("x"), nil))
	fake.Err = errors.New("forbidden")
	_, err := b.DeletePrefix(context.Background(), "p/")
	ErrContains(t, "forbidden", err)
}

func TestExistsAndMissingKeys(t *testing.T) {
	fake := objstoretest.New()
	b := objstore.NewBucket(fake, objstore.Config{Bucket: "b"})
	ok, err := b.Exists(context.Background(), "nope")
	Ok(t, err)
	Assert(t, !ok, "missing key must not exist")
	Ok(t, b.Delete(context.Background(), "nope")) // deleting a missing key is fine
	Ok(t, b.Put(context.Background(), "yes", strings.NewReader("x"), nil))
	ok, err = b.Exists(context.Background(), "yes")
	Ok(t, err)
	Assert(t, ok, "key must exist")
}

func TestDownloadIsAtomic(t *testing.T) {
	fake := objstoretest.New()
	b := objstore.NewBucket(fake, objstore.Config{Bucket: "b"})
	dst := filepath.Join(t.TempDir(), "nested", "plan.tfplan")
	Ok(t, b.Put(context.Background(), "k", strings.NewReader("new plan"), map[string]string{"head-commit": "abc"}))
	md, err := b.Download(context.Background(), "k", dst)
	Ok(t, err)
	Equals(t, "abc", md["head-commit"])
	body, err := os.ReadFile(dst)
	Ok(t, err)
	Equals(t, "new plan", string(body))

	// A failed download leaves the existing file untouched and no temp files.
	_, err = b.Download(context.Background(), "missing", dst)
	Assert(t, err != nil, "expected error")
	body, _ = os.ReadFile(dst)
	Equals(t, "new plan", string(body))
	entries, _ := os.ReadDir(filepath.Dir(dst))
	Equals(t, 1, len(entries))
}
