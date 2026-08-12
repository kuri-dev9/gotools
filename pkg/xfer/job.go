package xfer

import (
	"fmt"
	"time"
)

// JobMode identifies an ACTION operation.
type JobMode string

const (
	JobModeGet    JobMode = "get"
	JobModePut    JobMode = "put"
	JobModeDelete JobMode = "del"
)

// Job contains one section selected by ACTION.
type Job struct {
	Name     string
	Mode     JobMode
	Settings LegacySettings
}

// JobPlan preserves the ACTION execution order: READ, WRITE, REMOVE.
type JobPlan struct {
	Read   []Job
	Write  []Job
	Remove []Job
}

// LoadJobPlan loads every section registered in ACTION.
func LoadJobPlan(filename string, now time.Time) (JobPlan, error) {
	sections, err := readINI(filename)
	if err != nil {
		return JobPlan{}, err
	}
	action := sections["ACTION"]
	if len(action) == 0 {
		return JobPlan{}, fmt.Errorf("ACTION section is required")
	}

	plan := JobPlan{}
	plan.Read, err = loadActionJobs(filename, "get", JobModeGet, splitCSV(action["READ"]), now)
	if err != nil {
		return JobPlan{}, err
	}
	plan.Write, err = loadActionJobs(filename, "put", JobModePut, splitCSV(action["WRITE"]), now)
	if err != nil {
		return JobPlan{}, err
	}
	plan.Remove, err = loadActionJobs(filename, "rm", JobModeDelete, splitCSV(action["REMOVE"]), now)
	if err != nil {
		return JobPlan{}, err
	}
	if len(plan.Read)+len(plan.Write)+len(plan.Remove) == 0 {
		return JobPlan{}, fmt.Errorf("ACTION does not contain any jobs")
	}
	return plan, nil
}

func loadActionJobs(filename, command string, mode JobMode, names []string, now time.Time) ([]Job, error) {
	jobs := make([]Job, 0, len(names))
	for _, name := range names {
		settings, err := LoadLegacyINI(filename, command, name, now)
		if err != nil {
			return nil, fmt.Errorf("load %s job %q: %w", mode, name, err)
		}
		if err := validateActionJob(mode, settings); err != nil {
			return nil, fmt.Errorf("invalid %s job %q: %w", mode, name, err)
		}
		jobs = append(jobs, Job{Name: name, Mode: mode, Settings: settings})
	}
	return jobs, nil
}

func validateActionJob(mode JobMode, settings LegacySettings) error {
	if settings.SourceDir == "" {
		if mode == JobModePut {
			return fmt.Errorf("SRC_DIR is required")
		}
		return fmt.Errorf("SRC_DIR remote directory is required")
	}
	if settings.DestinationDir == "" && (mode == JobModeGet || mode == JobModePut) {
		return fmt.Errorf("DST_DIR is required")
	}
	if settings.FetchInterval <= 0 {
		return fmt.Errorf("FETCH_INTERVAL must be greater than zero")
	}
	return nil
}
