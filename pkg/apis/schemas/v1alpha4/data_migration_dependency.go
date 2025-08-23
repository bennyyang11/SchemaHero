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

package v1alpha4

import (
	"fmt"
	"sort"
)

// DependencyResolver resolves data migration dependencies and determines execution order
type DependencyResolver struct {
	migrations map[string]*DataMigration
	statuses   map[string]DataMigrationStatus
}

// NewDependencyResolver creates a new dependency resolver
func NewDependencyResolver(migrations []DataMigration) *DependencyResolver {
	migMap := make(map[string]*DataMigration)
	statusMap := make(map[string]DataMigrationStatus)

	for i := range migrations {
		migMap[migrations[i].Name] = &migrations[i]
		statusMap[migrations[i].Name] = DataMigrationPending
	}

	return &DependencyResolver{
		migrations: migMap,
		statuses:   statusMap,
	}
}

// ResolveExecutionOrder returns migrations in the order they should be executed
func (r *DependencyResolver) ResolveExecutionOrder() ([]*DataMigration, error) {
	// Check for circular dependencies
	if err := r.checkCircularDependencies(); err != nil {
		return nil, err
	}

	// Build execution order using topological sort
	var ordered []*DataMigration
	visited := make(map[string]bool)
	visiting := make(map[string]bool)

	// First, sort by priority (descending) and name for deterministic ordering
	var names []string
	for name := range r.migrations {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		mi := r.migrations[names[i]]
		mj := r.migrations[names[j]]
		if mi.Priority != mj.Priority {
			return mi.Priority > mj.Priority // Higher priority first
		}
		return mi.Name < mj.Name // Alphabetical for same priority
	})

	// Perform topological sort
	var visit func(name string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		if visiting[name] {
			return fmt.Errorf("circular dependency detected involving migration: %s", name)
		}

		visiting[name] = true
		migration := r.migrations[name]

		// Visit dependencies first
		for _, dep := range migration.DependsOn {
			if _, exists := r.migrations[dep]; !exists {
				return fmt.Errorf("migration %s depends on non-existent migration: %s", name, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}

		visiting[name] = false
		visited[name] = true
		ordered = append(ordered, migration)

		return nil
	}

	// Visit all migrations
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, err
		}
	}

	return ordered, nil
}

// checkCircularDependencies detects circular dependencies
func (r *DependencyResolver) checkCircularDependencies() error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	var hasCycle func(name string) (bool, []string)
	hasCycle = func(name string) (bool, []string) {
		visited[name] = true
		recStack[name] = true

		migration := r.migrations[name]
		for _, dep := range migration.DependsOn {
			if _, exists := r.migrations[dep]; !exists {
				continue // Will be caught later
			}

			if !visited[dep] {
				if cycle, path := hasCycle(dep); cycle {
					return true, append([]string{name}, path...)
				}
			} else if recStack[dep] {
				return true, []string{name, dep}
			}
		}

		recStack[name] = false
		return false, nil
	}

	for name := range r.migrations {
		if !visited[name] {
			if cycle, path := hasCycle(name); cycle {
				return fmt.Errorf("circular dependency detected: %v", path)
			}
		}
	}

	return nil
}

// GetExecutableMigrations returns migrations that can be executed now
func (r *DependencyResolver) GetExecutableMigrations() []*DataMigration {
	var executable []*DataMigration

	for name, migration := range r.migrations {
		if r.statuses[name] != DataMigrationPending {
			continue
		}

		if r.canExecute(migration) {
			executable = append(executable, migration)
		}
	}

	// Sort by priority and name for deterministic ordering
	sort.Slice(executable, func(i, j int) bool {
		if executable[i].Priority != executable[j].Priority {
			return executable[i].Priority > executable[j].Priority
		}
		return executable[i].Name < executable[j].Name
	})

	return executable
}

// canExecute checks if a migration can be executed based on dependencies
func (r *DependencyResolver) canExecute(migration *DataMigration) bool {
	// Check all dependencies are satisfied
	for _, dep := range migration.DependsOn {
		depStatus, exists := r.statuses[dep]
		if !exists {
			return false // Dependency doesn't exist
		}
		if depStatus != DataMigrationCompleted {
			return false // Dependency not completed
		}
	}

	return true
}

// UpdateStatus updates the status of a migration
func (r *DependencyResolver) UpdateStatus(name string, status DataMigrationStatus) error {
	if _, exists := r.migrations[name]; !exists {
		return fmt.Errorf("migration %s not found", name)
	}
	r.statuses[name] = status
	return nil
}

// GetDependencyGraph returns a visual representation of dependencies
func (r *DependencyResolver) GetDependencyGraph() map[string][]string {
	graph := make(map[string][]string)

	for name, migration := range r.migrations {
		graph[name] = migration.DependsOn
	}

	return graph
}

// ValidateDependencies checks all dependencies are valid
func (r *DependencyResolver) ValidateDependencies() error {
	for name, migration := range r.migrations {
		for _, dep := range migration.DependsOn {
			if _, exists := r.migrations[dep]; !exists {
				return fmt.Errorf("migration %s depends on non-existent migration: %s", name, dep)
			}
		}
	}

	return r.checkCircularDependencies()
}
