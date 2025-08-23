package schemaherokubectlcli

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/database"
	"github.com/schemahero/schemahero/pkg/database/types"
	"github.com/schemahero/schemahero/pkg/files"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v2"
)

// PlanResult holds the planning results for better formatting
type PlanResult struct {
	SourceFile        string
	DDLStatements     []string
	DMLStatements     []string
	EstimatedRows     int64
	EstimatedTime     time.Duration
	HasDataMigrations bool
}

func PlanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "plan",
		Short:        "plan a spec application against a database",
		Long:         `Generate and preview DDL and DML statements for database migrations`,
		SilenceUsage: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			v := viper.GetViper()

			// to support automaticenv, we can't use cobra required flags
			driver := v.GetString("driver")
			specFile := v.GetString("spec-file")
			uri := v.GetString("uri")
			host := v.GetStringSlice("host")

			if driver == "" || specFile == "" || uri == "" || len(host) == 0 {
				missing := []string{}
				if driver == "" {
					missing = append(missing, "driver")
				}
				if specFile == "" {
					missing = append(missing, "spec-file")
				}

				// one of uri or host/port must be specified
				if uri == "" && len(host) == 0 {
					missing = append(missing, "uri or host(s)")
				}

				if len(missing) > 0 {
					return fmt.Errorf("missing required params: %v", missing)
				}
			}

			fi, err := os.Stat(v.GetString("spec-file"))
			if err != nil {
				return err
			}

			if _, err = os.Stat(v.GetString("out")); err == nil {
				if !v.GetBool("overwrite") {
					return errors.Errorf("file %s already exists", v.GetString("out"))
				}

				err = os.RemoveAll(v.GetString("out"))
				if err != nil {
					return errors.Wrap(err, "failed remove existing file")
				}
			}

			var f *os.File
			if v.GetString("out") != "" {
				f, err = os.OpenFile(v.GetString("out"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
				if err != nil {
					return err
				}
				defer func() {
					f.Close()
				}()
			}

			db := database.Database{
				InputDir:       v.GetString("input-dir"),
				OutputDir:      v.GetString("output-dir"),
				Driver:         v.GetString("driver"),
				URI:            v.GetString("uri"),
				Hosts:          v.GetStringSlice("host"),
				Username:       v.GetString("username"),
				Password:       v.GetString("password"),
				Keyspace:       v.GetString("keyspace"),
				DeploySeedData: v.GetBool("seed-data"),
			}

			// Enhanced planning options
			dryRun := v.GetBool("dry-run")
			dataOnly := v.GetBool("data-migrations-only")
			schemaOnly := v.GetBool("schema-only")
			showMetrics := v.GetBool("show-metrics")
			verboseOutput := v.GetBool("verbose")

			specsFromFiles := []types.Spec{}
			if fi.Mode().IsDir() {
				err := filepath.Walk(v.GetString("spec-file"), func(path string, info os.FileInfo, err error) error {
					isHidden, err := files.IsHidden(path)
					if err != nil {
						return err
					}

					if info.IsDir() {
						if isHidden {
							return filepath.SkipDir
						}
						return nil
					}

					if isHidden {
						return nil
					}

					specContents, err := ioutil.ReadFile(filepath.Clean(path))
					if err != nil {
						return errors.Wrap(err, "failed to read file")
					}
					specsFromFiles = append(specsFromFiles, types.Spec{
						SourceFilename: path,
						Spec:           specContents,
					})

					return nil
				})
				if err != nil {
					return errors.Wrap(err, "failed to walk directory")
				}

				db.SortSpecs(specsFromFiles)

				results := []PlanResult{}
				for _, spec := range specsFromFiles {
					result, err := planSpecWithEnhancements(&db, spec, dryRun, dataOnly, schemaOnly, showMetrics)
					if err != nil {
						return fmt.Errorf("plan from file %q: %w", spec.SourceFilename, err)
					}
					results = append(results, result)
				}

				// Output results
				return outputPlanResults(results, f, verboseOutput, showMetrics, dryRun)

			} else {
				specContents, err := ioutil.ReadFile(v.GetString("spec-file"))
				if err != nil {
					return errors.Wrap(err, "failed to read spec file")
				}

				spec := types.Spec{
					SourceFilename: v.GetString("spec-file"),
					Spec:           specContents,
				}

				result, err := planSpecWithEnhancements(&db, spec, dryRun, dataOnly, schemaOnly, showMetrics)
				if err != nil {
					return fmt.Errorf("plan from file %q: %w", v.GetString("spec-file"), err)
				}

				// Output single result
				return outputPlanResults([]PlanResult{result}, f, verboseOutput, showMetrics, dryRun)
			}
		},
	}

	cmd.Flags().String("driver", "", "name of the database driver to use")

	cmd.Flags().String("uri", "", "connection string uri to use")

	cmd.Flags().String("username", "", "username to use when connecting")
	cmd.Flags().String("password", "", "password to use when connecting")
	cmd.Flags().StringSlice("host", []string{}, "hostname to use when connecting")
	cmd.Flags().String("keyspace", "", "the keyspace to use for databases that support keyspaces")

	cmd.Flags().String("spec-file", "", "filename or directory name containing the spec(s) to apply")
	cmd.Flags().String("spec-type", "table", "type of spec in spec-file (table, view, function, or extension)")
	cmd.Flags().String("out", "", "filename to write DDL statements to, if not present output file be written to stdout")
	cmd.Flags().Bool("overwrite", true, "when set, will overwrite the out file, if it already exists")

	cmd.Flags().Bool("seed-data", false, "when set, will deploy seed data")

	// Enhanced data migration flags
	cmd.Flags().Bool("dry-run", false, "when set, will show preview of changes without executing")
	cmd.Flags().Bool("data-migrations-only", false, "when set, will only plan data migrations (DML)")
	cmd.Flags().Bool("schema-only", false, "when set, will only plan schema changes (DDL)")
	cmd.Flags().Bool("show-metrics", false, "when set, will show estimated execution time and affected rows")
	cmd.Flags().Bool("verbose", false, "when set, will show detailed migration information")

	return cmd
}

// planSpecWithEnhancements performs enhanced planning for both DDL and DML
func planSpecWithEnhancements(db *database.Database, spec types.Spec, dryRun, dataOnly, schemaOnly, showMetrics bool) (PlanResult, error) {
	result := PlanResult{
		SourceFile:    spec.SourceFilename,
		DDLStatements: []string{},
		DMLStatements: []string{},
	}

	// Parse the spec to check if it has data migrations
	if hasDataMigrations, err := specHasDataMigrations(spec.Spec); err != nil {
		return result, err
	} else {
		result.HasDataMigrations = hasDataMigrations
	}

	// Use the enhanced planning method for tables with data migrations
	if result.HasDataMigrations && !schemaOnly {
		// Parse the spec to get TableSpec
		var tableSpec *schemasv1alpha4.TableSpec

		// Try Kubernetes object format first
		parsedK8sObject := schemasv1alpha4.Table{}
		if err := yaml.Unmarshal(spec.Spec, &parsedK8sObject); err == nil {
			tableSpec = &parsedK8sObject.Spec
		} else {
			// Try plain spec format
			plainSpec := schemasv1alpha4.TableSpec{}
			if err := yaml.Unmarshal(spec.Spec, &plainSpec); err != nil {
				return result, errors.Wrap(err, "failed to unmarshal table spec")
			}
			tableSpec = &plainSpec
		}

		ddlStatements, dmlStatements, err := db.PlanCompleteTableSpec(tableSpec)
		if err != nil {
			return result, errors.Wrap(err, "failed to plan complete table spec")
		}

		if !dataOnly {
			result.DDLStatements = ddlStatements
		}
		result.DMLStatements = dmlStatements

		// Estimate metrics if requested
		if showMetrics {
			estimatedRows, estimatedTime, err := estimateMigrationMetrics(db, spec.Spec, dmlStatements)
			if err == nil { // Don't fail if metrics estimation fails
				result.EstimatedRows = estimatedRows
				result.EstimatedTime = estimatedTime
			}
		}
	} else {
		// Fall back to traditional planning for schema-only or specs without data migrations
		if !dataOnly {
			statements, err := db.PlanSync(spec.Spec, "table")
			if err != nil {
				return result, errors.Wrap(err, "failed to plan sync")
			}
			result.DDLStatements = statements
		}
	}

	return result, nil
}

// specHasDataMigrations checks if a spec contains data migrations
func specHasDataMigrations(specContents []byte) (bool, error) {
	// Try to parse as Kubernetes object first
	parsedK8sObject := schemasv1alpha4.Table{}
	if err := yaml.Unmarshal(specContents, &parsedK8sObject); err == nil {
		// Check if we actually got meaningful K8s object data (has database and name populated)
		if parsedK8sObject.Spec.Database != "" && parsedK8sObject.Spec.Name != "" {
			return len(parsedK8sObject.Spec.DataMigrations) > 0, nil
		}
		// If database and name are empty, this is likely a plain spec format
	}

	// Try to parse as plain TableSpec
	plainSpec := schemasv1alpha4.TableSpec{}
	if err := yaml.Unmarshal(specContents, &plainSpec); err != nil {
		return false, errors.Wrap(err, "failed to unmarshal spec")
	}

	return len(plainSpec.DataMigrations) > 0, nil
}

// estimateMigrationMetrics estimates execution time and affected rows for data migrations
func estimateMigrationMetrics(db *database.Database, specContents []byte, dmlStatements []string) (int64, time.Duration, error) {
	// Parse spec to get data migrations
	var dataMigrations []schemasv1alpha4.DataMigration

	// Try Kubernetes object format first
	parsedK8sObject := schemasv1alpha4.Table{}
	if err := yaml.Unmarshal(specContents, &parsedK8sObject); err == nil {
		// Check if we actually got meaningful K8s object data (has database and name populated)
		if parsedK8sObject.Spec.Database != "" && parsedK8sObject.Spec.Name != "" {
			dataMigrations = parsedK8sObject.Spec.DataMigrations
		} else {
			// If database and name are empty, this is likely a plain spec format
			// Try plain spec format
			plainSpec := schemasv1alpha4.TableSpec{}
			if err := yaml.Unmarshal(specContents, &plainSpec); err != nil {
				return 0, 0, errors.Wrap(err, "failed to unmarshal spec for metrics")
			}
			dataMigrations = plainSpec.DataMigrations
		}
	} else {
		// Try plain spec format
		plainSpec := schemasv1alpha4.TableSpec{}
		if err := yaml.Unmarshal(specContents, &plainSpec); err != nil {
			return 0, 0, errors.Wrap(err, "failed to unmarshal spec for metrics")
		}
		dataMigrations = plainSpec.DataMigrations
	}

	// Estimate total rows affected and execution time
	var totalRows int64
	var totalTime time.Duration

	for _, migration := range dataMigrations {
		// Basic estimation: assume 1000 rows per migration unless batch size is specified
		estimatedRows := int64(1000)
		if migration.BatchSize > 0 {
			estimatedRows = int64(migration.BatchSize)
		}
		totalRows += estimatedRows

		// Basic time estimation: assume 100ms per batch
		estimatedTime := time.Millisecond * 100
		if migration.Timeout != nil {
			estimatedTime = migration.Timeout.Duration / 10 // Assume it takes 10% of timeout
		}
		totalTime += estimatedTime
	}

	return totalRows, totalTime, nil
}

// outputPlanResults formats and outputs the planning results
func outputPlanResults(results []PlanResult, f *os.File, verbose, showMetrics, dryRun bool) error {
	for i, result := range results {
		if len(results) > 1 {
			output := fmt.Sprintf("\n--- Migration Plan for %s ---\n", result.SourceFile)
			if err := writeOutput(f, output); err != nil {
				return err
			}
		}

		if dryRun {
			output := "-- DRY RUN MODE: No changes will be executed\n"
			if err := writeOutput(f, output); err != nil {
				return err
			}
		}

		// Show metrics if requested
		if showMetrics && result.HasDataMigrations {
			output := fmt.Sprintf("-- Migration Metrics:\n")
			output += fmt.Sprintf("--   Estimated rows affected: %d\n", result.EstimatedRows)
			output += fmt.Sprintf("--   Estimated execution time: %s\n", result.EstimatedTime)
			output += fmt.Sprintf("--   Has data migrations: %t\n", result.HasDataMigrations)
			if err := writeOutput(f, output+"\n"); err != nil {
				return err
			}
		}

		// Output DDL statements
		if len(result.DDLStatements) > 0 {
			if verbose {
				output := "-- ========================================\n"
				output += "-- SCHEMA CHANGES (DDL)\n"
				output += "-- ========================================\n"
				if err := writeOutput(f, output); err != nil {
					return err
				}
			}

			for _, statement := range result.DDLStatements {
				if err := writeOutput(f, fmt.Sprintf("%s;\n", statement)); err != nil {
					return err
				}
			}
		}

		// Output DML statements
		if len(result.DMLStatements) > 0 {
			if verbose {
				output := "\n-- ========================================\n"
				output += "-- DATA MIGRATIONS (DML)\n"
				output += "-- ========================================\n"
				if err := writeOutput(f, output); err != nil {
					return err
				}
			}

			for _, statement := range result.DMLStatements {
				if err := writeOutput(f, fmt.Sprintf("%s;\n", statement)); err != nil {
					return err
				}
			}
		}

		// Add separator between multiple results
		if i < len(results)-1 {
			if err := writeOutput(f, "\n"); err != nil {
				return err
			}
		}
	}

	return nil
}

// writeOutput writes to file or stdout
func writeOutput(f *os.File, content string) error {
	if f != nil {
		_, err := f.WriteString(content)
		return err
	}
	fmt.Print(content)
	return nil
}
