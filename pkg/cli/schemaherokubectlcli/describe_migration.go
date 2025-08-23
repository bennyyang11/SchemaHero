package schemaherokubectlcli

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	schemasclientv1alpha4 "github.com/schemahero/schemahero/pkg/client/schemaheroclientset/typed/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	corev1 "k8s.io/api/core/v1"
	kuberneteserrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func DescribeMigrationCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "migration",
		Short:         "",
		Long:          `...`,
		Args:          cobra.MinimumNArgs(1),
		SilenceErrors: true,
		SilenceUsage:  true,
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			v := viper.GetViper()
			ctx := context.Background()
			migrationName := args[0]

			cfg, err := config.GetRESTConfig()
			if err != nil {
				return err
			}

			client, err := kubernetes.NewForConfig(cfg)
			if err != nil {
				return err
			}

			schemasClient, err := schemasclientv1alpha4.NewForConfig(cfg)
			if err != nil {
				return err
			}

			namespaceNames := []string{}

			if viper.GetBool("all-namespaces") {
				namespaces, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}

				for _, namespace := range namespaces.Items {
					namespaceNames = append(namespaceNames, namespace.Name)
				}
			} else {
				if v.GetString("namespace") != "" {
					namespaceNames = []string{v.GetString("namespace")}
				} else {
					namespaceNames = []string{"default"}
				}
			}

			for _, namespaceName := range namespaceNames {
				foundMigration, err := schemasClient.Migrations(namespaceName).Get(ctx, migrationName, metav1.GetOptions{})
				if kuberneteserrors.IsNotFound(err) {
					// next namespace
					continue
				}
				if err != nil {
					return err
				}

				baseCommand := "kubectl schemahero"
				if namespaceName != corev1.NamespaceDefault {
					baseCommand = fmt.Sprintf("%s -n %s", baseCommand, namespaceName)
				}

				fmt.Printf("\nMigration Name: %s\n\n", foundMigration.Name)

				fmt.Printf("Generated DDL Statement (generated at %s): \n  %s\n",
					time.Unix(foundMigration.Status.PlannedAt, 0).Format(time.RFC3339),
					foundMigration.Spec.GeneratedDDL)

				// Display data migration information if present
				if foundMigration.Spec.GeneratedDML != "" {
					fmt.Printf("\nGenerated DML Statement: \n  %s\n", foundMigration.Spec.GeneratedDML)
				}

				// Display status information
				fmt.Printf("\nStatus: %s\n", foundMigration.Status.Phase)
				if foundMigration.Status.ApprovedAt > 0 {
					fmt.Printf("Approved at: %s\n", time.Unix(foundMigration.Status.ApprovedAt, 0).Format(time.RFC3339))
				}
				if foundMigration.Status.ExecutedAt > 0 {
					fmt.Printf("Applied at: %s\n", time.Unix(foundMigration.Status.ExecutedAt, 0).Format(time.RFC3339))
				}
				if foundMigration.Status.RejectedAt > 0 {
					fmt.Printf("Rejected at: %s\n", time.Unix(foundMigration.Status.RejectedAt, 0).Format(time.RFC3339))
				}

				// Enhanced data migration status reporting
				if foundMigration.Status.SchemaMigrationStatus != "" || foundMigration.Status.DataMigrationStatus != "" {
					fmt.Printf("\n--- Migration Phase Status ---\n")

					if foundMigration.Spec.GeneratedDDL != "" {
						fmt.Printf("Schema Migration Status: %s\n", foundMigration.Status.SchemaMigrationStatus)
					}

					if foundMigration.Spec.GeneratedDML != "" {
						fmt.Printf("Data Migration Status: %s\n", foundMigration.Status.DataMigrationStatus)

						if foundMigration.Status.EstimatedDataRows > 0 {
							fmt.Printf("Estimated Rows Affected: %d\n", foundMigration.Status.EstimatedDataRows)
						}

						if foundMigration.Status.EstimatedDuration != "" {
							fmt.Printf("Estimated Duration: %s\n", foundMigration.Status.EstimatedDuration)
						}
					}
				}

				// Display error information if any phase failed
				if foundMigration.Status.SchemaMigrationStatus == schemasv1alpha4.DataMigrationFailed ||
					foundMigration.Status.DataMigrationStatus == schemasv1alpha4.DataMigrationFailed {
					fmt.Printf("\n--- Error Information ---\n")
					fmt.Printf("⚠️  One or more migration phases failed. Check logs for details.\n")

					if foundMigration.Status.SchemaMigrationStatus == schemasv1alpha4.DataMigrationFailed {
						fmt.Printf("❌ Schema migration failed\n")
					}
					if foundMigration.Status.DataMigrationStatus == schemasv1alpha4.DataMigrationFailed {
						fmt.Printf("❌ Data migration failed\n")
					}
				}

				// Display progress information for running migrations
				if foundMigration.Status.SchemaMigrationStatus == schemasv1alpha4.DataMigrationRunning ||
					foundMigration.Status.DataMigrationStatus == schemasv1alpha4.DataMigrationRunning {
					fmt.Printf("\n--- Progress Information ---\n")
					fmt.Printf("🚀 Migration is currently running...\n")

					if foundMigration.Status.SchemaMigrationStatus == schemasv1alpha4.DataMigrationRunning {
						fmt.Printf("📊 Schema changes are being applied\n")
					}
					if foundMigration.Status.DataMigrationStatus == schemasv1alpha4.DataMigrationRunning {
						fmt.Printf("📊 Data migrations are being executed\n")
						if foundMigration.Status.EstimatedDataRows > 0 {
							fmt.Printf("📈 Processing up to %d rows\n", foundMigration.Status.EstimatedDataRows)
						}
					}
				}

				// Only show approval/action commands for migrations that haven't been approved or applied
				if foundMigration.Status.Phase == schemasv1alpha4.Planned {
					fmt.Println("")
					fmt.Println("To apply this migration:")
					fmt.Printf(`  %s approve migration %s`, baseCommand, foundMigration.Name)
					fmt.Println("")

					fmt.Println("")
					fmt.Println("To recalculate this migration against the current schema:")
					fmt.Printf(`  %s recalculate migration %s`, baseCommand, foundMigration.Name)
					fmt.Println("")

					fmt.Println("")
					fmt.Println("To deny and cancel this migration:")
					fmt.Printf(`  %s reject migration %s`, baseCommand, foundMigration.Name)
					fmt.Println("")
				}

				return nil
			}

			err = errors.Errorf("migration %q not found", migrationName)
			return err

		},
	}

	cmd.Flags().Bool("all-namespaces", false, "If present, list the requested object(s) across all namespaces. Namespace in current context is ignored even if specified with --namespace.")
	cmd.Flags().StringP("output", "o", "yaml", "Output format (can be json or yaml")

	return cmd
}
