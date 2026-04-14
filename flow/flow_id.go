package flow

// 本文件承载 SubProto 中 `flow` 模块里与 `flow_id` 相关的逻辑。

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

var flowIDPattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func validateFlowID(raw string) (string, error) {
	flowID := strings.TrimSpace(raw)
	if flowID == "" {
		return "", errors.New("flow_id required")
	}
	if !flowIDPattern.MatchString(flowID) {
		return "", errors.New("flow_id must be uuid")
	}
	return flowID, nil
}

func validateRunID(raw string) (string, error) {
	runID := strings.TrimSpace(raw)
	if runID == "" {
		return "", errors.New("run_id required")
	}
	if !flowIDPattern.MatchString(runID) {
		return "", errors.New("run_id must be uuid")
	}
	return runID, nil
}

func flowFilePath(baseDir, flowID string) (string, error) {
	baseDir = strings.TrimSpace(baseDir)
	if baseDir == "" {
		return "", errors.New("flow base_dir required")
	}
	validID, err := validateFlowID(flowID)
	if err != nil {
		return "", err
	}
	return filepath.Join(baseDir, validID+".json"), nil
}
