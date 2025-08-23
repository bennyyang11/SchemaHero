/*
Copyright 2025 The SchemaHero Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package migration

import (
	"sync"
	"time"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
)

// MigrationMetrics tracks performance metrics for data migrations
type MigrationMetrics struct {
	mu                   sync.RWMutex
	schemaMigrations     map[string]*MigrationPhaseMetrics
	dataMigrations       map[string]*MigrationPhaseMetrics
	totalMigrations      int64
	successfulMigrations int64
	failedMigrations     int64
	retriedMigrations    int64
}

// MigrationPhaseMetrics tracks metrics for a specific migration phase
type MigrationPhaseMetrics struct {
	MigrationName string
	Phase         string
	StartTime     time.Time
	EndTime       time.Time
	Duration      time.Duration
	RowsAffected  int64
	Status        schemasv1alpha4.DataMigrationStatus
	RetryCount    int
	ErrorMessage  string
}

// Global metrics instance
var globalMetrics = &MigrationMetrics{
	schemaMigrations: make(map[string]*MigrationPhaseMetrics),
	dataMigrations:   make(map[string]*MigrationPhaseMetrics),
}

// GetGlobalMetrics returns the global metrics instance
func GetGlobalMetrics() *MigrationMetrics {
	return globalMetrics
}

// StartSchemaMigration records the start of a schema migration
func (m *MigrationMetrics) StartSchemaMigration(migrationName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.schemaMigrations[migrationName] = &MigrationPhaseMetrics{
		MigrationName: migrationName,
		Phase:         "schema",
		StartTime:     time.Now(),
		Status:        schemasv1alpha4.DataMigrationRunning,
	}
}

// CompleteSchemaMigration records the completion of a schema migration
func (m *MigrationMetrics) CompleteSchemaMigration(migrationName string, status schemasv1alpha4.DataMigrationStatus, errorMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if metrics, exists := m.schemaMigrations[migrationName]; exists {
		metrics.EndTime = time.Now()
		metrics.Duration = metrics.EndTime.Sub(metrics.StartTime)
		metrics.Status = status
		metrics.ErrorMessage = errorMsg
	}
}

// StartDataMigration records the start of a data migration
func (m *MigrationMetrics) StartDataMigration(migrationName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.dataMigrations[migrationName] = &MigrationPhaseMetrics{
		MigrationName: migrationName,
		Phase:         "data",
		StartTime:     time.Now(),
		Status:        schemasv1alpha4.DataMigrationRunning,
	}
}

// CompleteDataMigration records the completion of a data migration
func (m *MigrationMetrics) CompleteDataMigration(migrationName string, status schemasv1alpha4.DataMigrationStatus, rowsAffected int64, errorMsg string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if metrics, exists := m.dataMigrations[migrationName]; exists {
		metrics.EndTime = time.Now()
		metrics.Duration = metrics.EndTime.Sub(metrics.StartTime)
		metrics.Status = status
		metrics.RowsAffected = rowsAffected
		metrics.ErrorMessage = errorMsg
	}

	// Update global counters
	m.totalMigrations++
	if status == schemasv1alpha4.DataMigrationCompleted {
		m.successfulMigrations++
	} else if status == schemasv1alpha4.DataMigrationFailed {
		m.failedMigrations++
	}
}

// RecordRetry records a migration retry attempt
func (m *MigrationMetrics) RecordRetry(migrationName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if metrics, exists := m.dataMigrations[migrationName]; exists {
		metrics.RetryCount++
	}
	m.retriedMigrations++
}

// GetSchemaMetrics returns metrics for schema migrations
func (m *MigrationMetrics) GetSchemaMetrics() map[string]*MigrationPhaseMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*MigrationPhaseMetrics)
	for k, v := range m.schemaMigrations {
		result[k] = v
	}
	return result
}

// GetDataMetrics returns metrics for data migrations
func (m *MigrationMetrics) GetDataMetrics() map[string]*MigrationPhaseMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]*MigrationPhaseMetrics)
	for k, v := range m.dataMigrations {
		result[k] = v
	}
	return result
}

// GetSummaryMetrics returns overall migration statistics
func (m *MigrationMetrics) GetSummaryMetrics() SummaryMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return SummaryMetrics{
		TotalMigrations:      m.totalMigrations,
		SuccessfulMigrations: m.successfulMigrations,
		FailedMigrations:     m.failedMigrations,
		RetriedMigrations:    m.retriedMigrations,
		SuccessRate:          calculateSuccessRate(m.successfulMigrations, m.totalMigrations),
	}
}

// SummaryMetrics provides high-level migration statistics
type SummaryMetrics struct {
	TotalMigrations      int64
	SuccessfulMigrations int64
	FailedMigrations     int64
	RetriedMigrations    int64
	SuccessRate          float64
}

// calculateSuccessRate calculates the success rate percentage
func calculateSuccessRate(successful, total int64) float64 {
	if total == 0 {
		return 0.0
	}
	return float64(successful) / float64(total) * 100.0
}

// ResetMetrics clears all collected metrics (useful for testing)
func (m *MigrationMetrics) ResetMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.schemaMigrations = make(map[string]*MigrationPhaseMetrics)
	m.dataMigrations = make(map[string]*MigrationPhaseMetrics)
	m.totalMigrations = 0
	m.successfulMigrations = 0
	m.failedMigrations = 0
	m.retriedMigrations = 0
}

// GetAverageExecutionTime returns the average execution time for a migration type
func (m *MigrationMetrics) GetAverageExecutionTime(phase string) time.Duration {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var totalDuration time.Duration
	var count int

	var metrics map[string]*MigrationPhaseMetrics
	if phase == "schema" {
		metrics = m.schemaMigrations
	} else {
		metrics = m.dataMigrations
	}

	for _, metric := range metrics {
		if metric.Status == schemasv1alpha4.DataMigrationCompleted && !metric.EndTime.IsZero() {
			totalDuration += metric.Duration
			count++
		}
	}

	if count == 0 {
		return time.Duration(0)
	}

	return totalDuration / time.Duration(count)
}
