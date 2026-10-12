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
	if err := os.MkdirAll(dir, 0700); err != nil {
		return Proof{}, fmt.Errorf("create proof directory: %w", err)
	}
	data, err := json.MarshalIndent(proof, "", "  ")
	if err != nil {
		return Proof{}, fmt.Errorf("encode proof: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, proof.ID+".json"), data, 0600); err != nil {
		return Proof{}, fmt.Errorf("write proof: %w", err)
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
	data, err := os.ReadFile(filepath.Join(root, ".aicli", "proofs", id+".json"))
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
