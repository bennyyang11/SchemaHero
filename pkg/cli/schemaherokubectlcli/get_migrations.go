package schemaherokubectlcli

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	schemasv1alpha4 "github.com/schemahero/schemahero/pkg/apis/schemas/v1alpha4"
	schemasclientv1alpha4 "github.com/schemahero/schemahero/pkg/client/schemaheroclientset/typed/schemas/v1alpha4"
	"github.com/schemahero/schemahero/pkg/config"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func GetMigrationsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "migrations",
		Short:         "",
		Long:          `...`,
		SilenceErrors: true,
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			v := viper.GetViper()
			ctx := context.Background()

			databaseNameFilter := v.GetString("database")

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

			matchingMigrations := []schemasv1alpha4.Migration{}
			for _, namespaceName := range namespaceNames {
				migrations, err := schemasClient.Migrations(namespaceName).List(ctx, metav1.ListOptions{})
				if err != nil {
					return err
				}

				for _, m := range migrations.Items {
					if databaseNameFilter == "" {
						matchingMigrations = append(matchingMigrations, m)
						continue
					}

					if m.Spec.DatabaseName == databaseNameFilter {
						matchingMigrations = append(matchingMigrations, m)
					}
				}
			}

			if len(matchingMigrations) == 0 {
				fmt.Println("No resources found.")
				return nil
			}

			rows := [][]string{}
			for _, m := range matchingMigrations {
				// Determine migration type and status
				migrationType := "DDL"
				migrationStatus := string(m.Status.Phase)

				if m.Spec.GeneratedDML != "" {
					if m.Spec.GeneratedDDL != "" {
						migrationType = "DDL+DML"
					} else {
						migrationType = "DML"
					}

					// Show more detailed status for data migrations
					if m.Status.DataMigrationStatus != "" {
						schemaStatus := string(m.Status.SchemaMigrationStatus)
						dataStatus := string(m.Status.DataMigrationStatus)
						if schemaStatus != "" && dataStatus != "" {
							migrationStatus = fmt.Sprintf("S:%s D:%s",
								shortenStatus(schemaStatus),
								shortenStatus(dataStatus))
						} else if dataStatus != "" {
							migrationStatus = shortenStatus(dataStatus)
						}
					}
				}

				rows = append(rows, []string{
					m.Name,
					m.Spec.DatabaseName,
					m.Spec.TableName,
					migrationType,
					migrationStatus,
					timestampToAge(m.Status.PlannedAt),
					timestampToAge(m.Status.ExecutedAt),
					timestampToAge(m.Status.ApprovedAt),
				})
			}

			if len(rows) == 0 {
				fmt.Println("No resources found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tDATABASE\tTABLE\tTYPE\tSTATUS\tPLANNED\tEXECUTED\tAPPROVED")

			for _, row := range rows {
				fmt.Fprintln(w, fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s", row[0], row[1], row[2], row[3], row[4], row[5], row[6], row[7]))
			}
			w.Flush()

			return nil
		},
	}

	cmd.Flags().StringP("database", "d", "", "database name to filter to results to")
	cmd.Flags().Bool("all-namespaces", false, "If present, list the requested object(s) across all namespaces. Namespace in current context is ignored even if specified with --namespace.")

	// cmd.Flags().StringP("status", "s", "", "status to filter to results to")

	return cmd
}

// shortenStatus provides a shortened version of migration status for table display
func shortenStatus(status string) string {
	switch status {
	case "PENDING":
		return "PEND"
	case "RUNNING":
		return "RUN"
	case "COMPLETED":
		return "COMP"
	case "FAILED":
		return "FAIL"
	case "SKIPPED":
		return "SKIP"
	case "ROLLED_BACK":
		return "ROLL"
	default:
		if len(status) > 4 {
			return status[:4]
		}
		return status
	}
}

func timestampToAge(t int64) string {
	if t == 0 {
		return ""
	}

	d := time.Since(time.Unix(t, 0))
	if d < time.Duration(time.Minute) {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	} else if d < time.Duration(time.Minute*10) {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	} else if d < time.Duration(time.Hour) {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	} else if d < time.Duration(time.Hour*24) {
		return fmt.Sprintf("%dh", int(d.Hours()))
	} else {
		return fmt.Sprintf("%dd%dh", int(d.Hours()/24), int(d.Hours())%24)
	}
}
