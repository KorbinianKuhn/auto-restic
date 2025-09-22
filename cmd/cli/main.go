package main

import (
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"text/tabwriter"

	"filippo.io/age"
	"github.com/korbiniankuhn/auto-restic/internal/config"
	"github.com/korbiniankuhn/auto-restic/internal/restic"
	"github.com/korbiniankuhn/auto-restic/internal/s3"
	"github.com/korbiniankuhn/auto-restic/internal/utils"
	"github.com/spf13/cobra"
)

type ctxKey string

const (
	ctxKeySession ctxKey = "session"
)

type Session struct {
	Config config.Config
	Restic restic.Restic
	S3     *s3.S3
}

func panicOnError(message string, err error) {
	if err != nil {
		println(message, err)
		panic(err)
	}
}

func initConfigAndLogging() config.Config {
	// Load config
	c, err := config.Get()
	panicOnError("failed to load config", err)

	return c
}

func initRestic(c config.Config) restic.Restic {
	// Initialize restic
	r, err := restic.NewRestic(c.Restic.Repository, c.Restic.Password)
	panicOnError("failed to initialize restic", err)
	return r
}

func initS3(c config.Config) *s3.S3 {
	s, err := s3.Get(c.S3.AccessKey, c.S3.SecretKey, c.S3.Endpoint, c.S3.Bucket)
	panicOnError("failed to initialize s3", err)
	return s
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "auto-restic",
		Short: "AutoRestic backup tool",
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			c := initConfigAndLogging()
			session := &Session{Config: c}

			if cmd.Parent() != nil && cmd.Parent().Name() == "restic" {
				session.Restic = initRestic(c)
			}

			if cmd.Parent() != nil && cmd.Parent().Name() == "s3" {
				session.S3 = initS3(c)
			}

			ctx := context.WithValue(cmd.Context(), ctxKeySession, session)
			cmd.SetContext(ctx)
			return nil
		},
	}

	resticCmd := &cobra.Command{
		Use:   "restic",
		Short: "Manage restic backups",
	}

	resticCmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List restic backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			session := cmd.Context().Value(ctxKeySession).(*Session)

			snapshots, err := session.Restic.ListSnapshots()
			if err != nil {
				return fmt.Errorf("failed to list restic snapshots: %w", err)
			}

			sort.Slice(snapshots, func(i, j int) bool {
				if snapshots[i].Name == snapshots[j].Name {
					return snapshots[i].Time.After(snapshots[j].Time)
				}
				return snapshots[i].Name < snapshots[j].Name
			})

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "Name\tDate\tID")
			fmt.Fprintln(w, "----\t----\t--")
			for _, s := range snapshots {
				fmt.Fprintf(w, "%s\t%s\t%s\n", s.Name, s.Time.Format("2006-01-02 15:04:05"), s.ID)
			}
			w.Flush()
			return nil
		},
	})

	resticRemoveCmd := &cobra.Command{
		Use:   "rm",
		Short: "Remove restic backups by name",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			backupName, _ := cmd.Flags().GetString("name")

			session := cmd.Context().Value(ctxKeySession).(*Session)

			output, err := session.Restic.RemoveBackupDirectory(backupName)
			if err != nil {
				return fmt.Errorf("failed to remove restic snapshots: %w", err)
			}
			println(output)
			println("Removed backup:", backupName)
			return nil
		},
	}
	resticRemoveCmd.Flags().String("name", "", "Name of the backup to remove")
	resticRemoveCmd.MarkFlagRequired("name")
	resticCmd.AddCommand(resticRemoveCmd)

	resticRestoreCmd := &cobra.Command{
		Use:   "restore",
		Short: "Restore a restic backup by snapshot ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			snapshotID, _ := cmd.Flags().GetString("snapshot-id")
			mountPath, _ := cmd.Flags().GetString("mount-path")
			session := cmd.Context().Value(ctxKeySession).(*Session)

			err := session.Restic.Restore(snapshotID, mountPath)
			if err != nil {
				return fmt.Errorf("failed to restore restic snapshot: %w", err)
			}

			println("Restored snapshot", snapshotID, "to", mountPath)
			return nil
		},
	}
	resticRestoreCmd.Flags().String("snapshot-id", "", "ID of the snapshot to restore")
	resticRestoreCmd.Flags().String("mount-path", "", "Local path to restore snapshot to")
	resticRestoreCmd.MarkFlagRequired("snapshot-id")
	resticRestoreCmd.MarkFlagRequired("mount-path")
	resticCmd.AddCommand(resticRestoreCmd)

	s3Cmd := &cobra.Command{
		Use:   "s3",
		Short: "Manage S3 backups",
	}

	s3Cmd.AddCommand(&cobra.Command{
		Use:   "ls",
		Short: "List S3 backups",
		RunE: func(cmd *cobra.Command, args []string) error {
			session := cmd.Context().Value(ctxKeySession).(*Session)

			objects, err := session.S3.ListObjects()
			if err != nil {
				return fmt.Errorf("failed to list S3 objects: %w", err)
			}

			sort.Slice(objects, func(i, j int) bool {
				if objects[i].BackupName == objects[j].BackupName {
					return objects[i].CreatedAt.After(objects[j].CreatedAt)
				}
				return objects[i].BackupName < objects[j].BackupName
			})

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "Object-Key\tDate\tVersion\tSize")
			fmt.Fprintln(w, "----------\t----\t-------\t----")
			for _, o := range objects {
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\n", o.Key, o.CreatedAt.Format("2006-01-02 15:04:05"), o.VersionID, o.Size)
			}
			w.Flush()
			return nil
		},
	})

	s3RemoveCmd := &cobra.Command{
		Use:   "rm",
		Short: "Remove an S3 object",
		RunE: func(cmd *cobra.Command, args []string) error {
			objectKey, _ := cmd.Flags().GetString("object-key")
			versionID, _ := cmd.Flags().GetString("version-id")
			session := cmd.Context().Value(ctxKeySession).(*Session)

			err := session.S3.RemoveObject(objectKey, versionID)
			if err != nil {
				return fmt.Errorf("failed to remove S3 object: %w", err)
			}

			println("Removed S3 object:", objectKey)
			return nil
		},
	}
	s3RemoveCmd.Flags().String("object-key", "", "Key of the S3 object to remove")
	s3RemoveCmd.Flags().String("version-id", "", "Version ID of the S3 object to remove")
	s3RemoveCmd.MarkFlagRequired("object-key")
	s3RemoveCmd.MarkFlagRequired("version-id")
	s3Cmd.AddCommand(s3RemoveCmd)

	s3RestoreCmd := &cobra.Command{
		Use:   "restore",
		Short: "Download an S3 object to a local file",
		RunE: func(cmd *cobra.Command, args []string) error {
			objectKey, _ := cmd.Flags().GetString("object-key")
			versionID, _ := cmd.Flags().GetString("version-id")
			mountPath, _ := cmd.Flags().GetString("mount-path")
			session := cmd.Context().Value(ctxKeySession).(*Session)

			decryptedPath := path.Join(mountPath, strings.TrimSuffix(objectKey, ".tar.gz.age"))

			// Create age identity
			identity, err := age.NewScryptIdentity(session.Config.S3.Passphrase)
			if err != nil {
				return fmt.Errorf("failed to create age identity: %w", err)
			}

			// Get a reader for the S3 object (streaming)
			s3Reader, err := session.S3.StreamDownloadFile(objectKey, versionID)
			if err != nil {
				return fmt.Errorf("failed to get S3 stream: %w", err)
			}
			defer s3Reader.Close()

			// Decrypt stream
			decReader, err := age.Decrypt(s3Reader, identity)
			if err != nil {
				return fmt.Errorf("failed to decrypt stream: %w", err)
			}

			// Gzip reader
			gzReader, err := gzip.NewReader(decReader)
			if err != nil {
				return fmt.Errorf("failed to create gzip reader: %w", err)
			}
			defer gzReader.Close()

			// Tar extraction
			utils.ExtractTar(gzReader, decryptedPath)

			println("Restored S3 object:", objectKey, "to", decryptedPath)
			return nil
		},
	}
	s3RestoreCmd.Flags().String("object-key", "", "Key of the S3 object to restore")
	s3RestoreCmd.Flags().String("version-id", "", "Version ID of the S3 object to restore")
	s3RestoreCmd.Flags().String("mount-path", "", "Local directory to restore the S3 object to")
	s3RestoreCmd.MarkFlagRequired("object-key")
	s3RestoreCmd.MarkFlagRequired("version-id")
	s3RestoreCmd.MarkFlagRequired("mount-path")
	s3Cmd.AddCommand(s3RestoreCmd)

	rootCmd.AddCommand(resticCmd, s3Cmd)

	if err := rootCmd.Execute(); err != nil {
		fmt.Println("command execution failed:", err)
		os.Exit(1)
	}
}
