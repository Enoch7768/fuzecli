package platform

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/google/uuid"
)

var proofIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)

func validateProofID(id string) error {
	if !proofIDPattern.MatchString(id) {
		return errors.New("invalid proof id")
	}
	return nil
}

func SaveProof(root string, proof Proof) (Proof, error) {
	if proof.ID == "" {
		proof.ID = uuid.NewString()
	}
	if err := validateProofID(proof.ID); err != nil {
		return Proof{}, err
	}
	if root == "" {
		return Proof{}, errors.New("workspace root is required")
	}
	if proof.CreatedAt.IsZero() {
		proof.CreatedAt = time.Now().UTC()
	}
	dir := filepath.Join(root, ".aicli", "proofs")
	if err := rejectSymlinkComponents(root, dir); err != nil {
		return Proof{}, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Proof{}, fmt.Errorf("create proof directory: %w", err)
	}
	path := filepath.Join(dir, proof.ID+".json")
	if err := rejectSymlinkComponents(root, path); err != nil {
		return Proof{}, err
	}
	data, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return Proof{}, fmt.Errorf("encode proof: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return Proof{}, fmt.Errorf("create proof: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return Proof{}, fmt.Errorf("write proof: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return Proof{}, fmt.Errorf("close proof: %w", err)
	}
	return proof, nil
}

func LoadProof(root, id string) (Proof, error) {
	if root == "" {
		return Proof{}, errors.New("workspace root is required")
	}
	if err := validateProofID(id); err != nil {
		return Proof{}, err
	}
	path := filepath.Join(root, ".aicli", "proofs", id+".json")
	if err := rejectSymlinkComponents(root, path); err != nil {
		return Proof{}, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return Proof{}, err
	}
	if !info.Mode().IsRegular() {
		return Proof{}, errors.New("proof must be a regular file")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Proof{}, err
	}
	var proof Proof
	if err := json.Unmarshal(data, &proof); err != nil {
		return Proof{}, fmt.Errorf("decode proof: %w", err)
	}
	if proof.ID != id {
		return Proof{}, errors.New("proof id does not match requested id")
	}
	return proof, nil
}
