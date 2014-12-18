package gitreader

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
)

type Loader interface {
	LoadObject(id string) (*Object, error)
}

type Repo struct {
	Base    string
	Loaders []Loader
}

func OpenRepo(path string) (*Repo, error) {
	dir := filepath.Join(path, ".git", "objects")

	if _, err := os.Stat(dir); err != nil {
		return nil, err
	}

	repo := &Repo{filepath.Join(path, ".git"), nil}

	err := repo.initLoaders()
	if err != nil {
		return nil, err
	}

	return repo, nil
}

func (r *Repo) initLoaders() error {
	loaders := []Loader{&LooseObject{r.Base}}

	packs := filepath.Join(r.Base, "objects/pack")

	files, err := ioutil.ReadDir(packs)
	if err == nil {
		for _, file := range files {
			n := file.Name()
			if filepath.Ext(n) == ".idx" {
				pack, err := LoadPack(filepath.Join(packs, n[:len(n)-4]))
				if err != nil {
					return err
				}

				loaders = append(loaders, pack)
			}
		}
	}

	r.Loaders = loaders

	return nil
}

var refDirs = []string{"heads", "tags"}

var ErrUnknownRef = errors.New("unknown ref")

func (r *Repo) ResolveRef(ref string) (string, error) {
	if ref == "HEAD" {
		return r.resolveIndirect(filepath.Join(r.Base, "HEAD"))
	}

	for _, dir := range refDirs {
		path := filepath.Join(r.Base, "refs", dir, ref)

		data, err := ioutil.ReadFile(path)
		if err != nil {
			continue
		}

		return strings.TrimSpace(string(data)), nil
	}

	path := filepath.Join(r.Base, ref)
	data, err := ioutil.ReadFile(path)
	if err == nil {
		return strings.TrimSpace(string(data)), nil
	}

	return "", ErrUnknownRef
}

func (r *Repo) resolveIndirect(path string) (string, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return "", err
	}

	id := strings.TrimSpace(string(data))

	if id[0:4] == "ref:" {
		return r.ResolveRef(strings.TrimSpace(id[4:]))
	}

	return id, nil
}

func (r *Repo) LoadObject(id string) (*Object, error) {
	for _, loader := range r.Loaders {
		obj, err := loader.LoadObject(id)
		if err != nil {
			if err == ErrNotExist {
				continue
			}

			return nil, err
		}

		return obj, nil
	}

	return nil, ErrNotExist
}

var ErrNotCommit = errors.New("ref is not a commit")
var ErrNotTree = errors.New("object is not a tree")
var ErrNotBlob = errors.New("object is not a blob")

func (r *Repo) Resolve(ref, path string) (string, error) {
	refId, err := r.ResolveRef(ref)
	if err != nil {
		return "", err
	}

	obj, err := r.LoadObject(refId)
	if err != nil {
		return "", err
	}

	if obj.Type != "commit" {
		return "", ErrNotCommit
	}

	commit, err := obj.Commit()
	if err != nil {
		return "", err
	}

	treeObj, err := r.LoadObject(commit.Tree)
	if err != nil {
		return "", err
	}

	if treeObj.Type != "tree" {
		return "", ErrNotTree
	}

	tree, err := treeObj.Tree()
	if err != nil {
		return "", err
	}

	segments := strings.Split(path, "/")

	for _, seg := range segments[:len(segments)-1] {
		nextId, ok := tree.Entries[seg]
		if !ok {
			return "", ErrNotExist
		}

		obj, err := r.LoadObject(nextId.Id)
		if err != nil {
			return "", err
		}

		if obj.Type != "tree" {
			return "", ErrNotTree
		}

		tree, err = obj.Tree()
		if err != nil {
			return "", err
		}
	}

	final := segments[len(segments)-1]

	nextId, ok := tree.Entries[final]
	if !ok {
		return "", ErrNotExist
	}

	return nextId.Id, nil
}

func (r *Repo) CatFile(ref, path string) (*Blob, error) {
	id, err := r.Resolve(ref, path)
	if err != nil {
		return nil, err
	}

	obj, err := r.LoadObject(id)
	if err != nil {
		return nil, err
	}

	if obj.Type != "blob" {
		return nil, ErrNotBlob
	}

	return obj.Blob()
}