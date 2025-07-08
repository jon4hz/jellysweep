package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/charmbracelet/log"
	"github.com/go-co-op/gocron/v2"
	"github.com/patrickmn/go-cache"
)

// JobStatus represents the status of a job.
type JobStatus string

const (
	JobStatusRunning   JobStatus = "running"
	JobStatusCompleted JobStatus = "completed"
	JobStatusFailed    JobStatus = "failed"
	JobStatusScheduled JobStatus = "scheduled"
)

// JobInfo contains information about a scheduled job.
type JobInfo struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Status      JobStatus  `json:"status"`
	LastRun     time.Time  `json:"lastRun"`
	NextRun     time.Time  `json:"nextRun"`
	Schedule    string     `json:"schedule"`
	Enabled     bool       `json:"enabled"`
	RunCount    int        `json:"runCount"`
	ErrorCount  int        `json:"errorCount"`
	LastError   string     `json:"lastError,omitempty"`
	Singleton   bool       `json:"singleton"`
	GocronJob   gocron.Job `json:"-"` // Store gocron job reference, exclude from JSON
}

// JobFunc represents a function that can be scheduled.
type JobFunc func(ctx context.Context) error

// Scheduler manages scheduled jobs and shared cache.
type Scheduler struct {
	gocron   gocron.Scheduler
	cache    *cache.Cache
	jobs     map[string]*JobInfo
	jobFuncs map[string]JobFunc
	ctx      context.Context
	cancel   context.CancelFunc
}

// New creates a new scheduler with shared cache.
func New() (*Scheduler, error) {
	gocronScheduler, err := gocron.NewScheduler(gocron.WithLogger(newLogger()))
	if err != nil {
		return nil, fmt.Errorf("failed to create gocron scheduler: %w", err)
	}

	// Create shared cache with 5 minute default expiration and 10 minute cleanup interval
	sharedCache := cache.New(5*time.Minute, 10*time.Minute)

	ctx, cancel := context.WithCancel(context.Background())

	return &Scheduler{
		gocron:   gocronScheduler,
		cache:    sharedCache,
		jobs:     make(map[string]*JobInfo),
		jobFuncs: make(map[string]JobFunc),
		ctx:      ctx,
		cancel:   cancel,
	}, nil
}

// GetCache returns the shared cache instance.
func (s *Scheduler) GetCache() *cache.Cache {
	return s.cache
}

// Start starts the scheduler.
func (s *Scheduler) Start() {
	log.Info("Starting job scheduler")
	s.gocron.Start()

	// after starting the scheduler, populate the next run times for all jobs
	for id, jobInfo := range s.jobs {
		if jobInfo.GocronJob != nil {
			if nextRun, err := jobInfo.GocronJob.NextRun(); err == nil {
				jobInfo.NextRun = nextRun
				log.Debug("Next run time for job", "id", id, "nextRun", nextRun)
			} else {
				log.Warn("Failed to get next run time for job", "id", id, "error", err)
			}
		} else {
			log.Warn("Gocron job reference not found for job", "id", id)
		}
	}
}

// Stop stops the scheduler.
func (s *Scheduler) Stop() error {
	log.Info("Stopping job scheduler")
	s.cancel()
	return s.gocron.Shutdown()
}

// AddJob adds a new job to the scheduler.
func (s *Scheduler) AddJob(id, name, description, definitionString string, jobDef gocron.JobDefinition, jobFunc JobFunc) error {
	return s.AddJobWithOptions(id, name, description, definitionString, jobDef, jobFunc, false)
}

// AddSingletonJob adds a new singleton job to the scheduler that can only run one instance at a time.
func (s *Scheduler) AddSingletonJob(id, name, description, definitionString string, jobDef gocron.JobDefinition, jobFunc JobFunc) error {
	return s.AddJobWithOptions(id, name, description, definitionString, jobDef, jobFunc, true)
}

// AddJobWithOptions adds a new job to the scheduler with optional singleton behavior.
func (s *Scheduler) AddJobWithOptions(id, name, description, definitionString string, jobDef gocron.JobDefinition, jobFunc JobFunc, singleton bool) error {
	// Store the job function
	s.jobFuncs[id] = jobFunc

	// Create job info
	jobInfo := &JobInfo{
		ID:          id,
		Name:        name,
		Description: description,
		Status:      JobStatusScheduled,
		Schedule:    definitionString,
		Enabled:     true,
		RunCount:    0,
		ErrorCount:  0,
		Singleton:   singleton,
	}

	// Wrap the job function to update job info
	wrappedFunc := s.wrapJobFunc(id, jobFunc)

	// Create job options
	var jobOptions []gocron.JobOption
	if singleton {
		// Use gocron's singleton mode with reschedule behavior
		jobOptions = append(jobOptions, gocron.WithSingletonMode(gocron.LimitModeReschedule))
	}

	// Create the gocron job
	job, err := s.gocron.NewJob(jobDef, gocron.NewTask(wrappedFunc), jobOptions...)
	if err != nil {
		return fmt.Errorf("failed to create job %s: %w", id, err)
	}

	// Store the gocron job reference in JobInfo
	jobInfo.GocronJob = job

	// Update next run time
	if nextRun, err := job.NextRun(); err == nil {
		jobInfo.NextRun = nextRun
		log.Debug("Next run time for job", "id", id, "nextRun", nextRun)
	} else {
		log.Warn("Failed to get next run time for job", "id", id, "error", err)
	}

	s.jobs[id] = jobInfo
	log.Info("Added job to scheduler", "id", id, "name", name, "singleton", singleton)
	return nil
}

// RunJobNow manually triggers a job to run immediately.
func (s *Scheduler) RunJobNow(id string) error {
	jobInfo, exists := s.jobs[id]
	if !exists {
		return fmt.Errorf("job %s not found", id)
	}

	if jobInfo.GocronJob == nil {
		return fmt.Errorf("gocron job reference not found for job %s", id)
	}

	log.Info("Manually triggering job", "id", id, "name", jobInfo.Name)

	// Use gocron's RunNow method to trigger the job
	if err := jobInfo.GocronJob.RunNow(); err != nil {
		return fmt.Errorf("failed to trigger job %s: %w", id, err)
	}

	return nil
}

// GetJobs returns all job information.
func (s *Scheduler) GetJobs() map[string]*JobInfo {
	return s.jobs
}

// GetJob returns information about a specific job.
func (s *Scheduler) GetJob(id string) (*JobInfo, bool) {
	job, exists := s.jobs[id]
	return job, exists
}

// EnableJob enables a job.
func (s *Scheduler) EnableJob(id string) error {
	jobInfo, exists := s.jobs[id]
	if !exists {
		return fmt.Errorf("job %s not found", id)
	}

	jobInfo.Enabled = true
	if nextRun, err := jobInfo.GocronJob.NextRun(); err == nil {
		jobInfo.NextRun = nextRun
	}

	log.Info("Enabled job", "id", id, "name", jobInfo.Name)
	return nil
}

// DisableJob disables a job.
func (s *Scheduler) DisableJob(id string) error {
	jobInfo, exists := s.jobs[id]
	if !exists {
		return fmt.Errorf("job %s not found", id)
	}

	jobInfo.Enabled = false
	log.Info("Disabled job", "id", id, "name", jobInfo.Name)
	return nil
}

// wrapJobFunc wraps a job function to update job statistics.
func (s *Scheduler) wrapJobFunc(id string, jobFunc JobFunc) func() {
	return func() {
		jobInfo := s.jobs[id]
		if jobInfo == nil {
			log.Error("Job info not found", "id", id)
			return
		}

		// Check if job is enabled
		if !jobInfo.Enabled {
			log.Debug("Job is disabled, skipping", "id", id)
			return
		}

		log.Info("Starting job", "id", id, "name", jobInfo.Name)
		jobInfo.Status = JobStatusRunning
		jobInfo.LastRun = time.Now()
		if nextRun, err := jobInfo.GocronJob.NextRun(); err == nil {
			jobInfo.NextRun = nextRun
		}
		jobInfo.RunCount++

		// Run the job
		if err := jobFunc(s.ctx); err != nil {
			log.Error("Job failed", "id", id, "name", jobInfo.Name, "error", err)
			jobInfo.Status = JobStatusFailed
			jobInfo.ErrorCount++
			jobInfo.LastError = err.Error()
		} else {
			log.Info("Job completed successfully", "id", id, "name", jobInfo.Name)
			jobInfo.Status = JobStatusCompleted
			jobInfo.LastError = ""
		}
	}
}

// ClearCache clears all cached data.
func (s *Scheduler) ClearCache() {
	s.cache.Flush()
	log.Info("Cleared all cached data")
}

// GetCacheStats returns cache statistics.
func (s *Scheduler) GetCacheStats() map[string]any {
	return map[string]any{
		"itemCount": s.cache.ItemCount(),
	}
}
