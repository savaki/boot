package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/kms"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/pkg/errors"
	"github.com/savaki/loggly"
	"github.com/urfave/cli"
)

const (
	EncSuffix        = ".enc"
	maxEncryptedSize = 4000
)

type Options struct {
	Region      string
	Env         string
	File        string
	Revision    string
	KmsID       string
	S3Bucket    string
	S3Prefix    string
	Dir         string
	Verbose     bool
	DryRun      bool
	LogglyToken string
}

var opts Options

func main() {
	var logglyFlag = cli.StringFlag{
		Name:        "token",
		Usage:       "the loggly token (re",
		EnvVar:      "LOGGLY_TOKEN",
		Destination: &opts.LogglyToken,
	}

	flags := []cli.Flag{
		cli.StringFlag{
			Name:        "region",
			Usage:       "aws region to use",
			Value:       "us-east-1",
			EnvVar:      "AWS_DEFAULT_REGION",
			Destination: &opts.Region,
		},
		cli.StringFlag{
			Name:        "env",
			Usage:       "environment",
			Value:       "dev",
			EnvVar:      "BOOT_ENV",
			Destination: &opts.Env,
		},
		cli.StringFlag{
			Name:        "file",
			Usage:       "environment variables will be stored in this file",
			Value:       "boot.env",
			EnvVar:      "BOOT_FILE",
			Destination: &opts.File,
		},
		cli.StringFlag{
			Name:        "revision",
			Usage:       "the name of the version to use",
			Value:       "latest",
			EnvVar:      "BOOT_REVISION",
			Destination: &opts.Revision,
		},
		cli.StringFlag{
			Name:        "kms",
			Usage:       "[REQUIRED] the kms key id",
			EnvVar:      "BOOT_KMS_ID",
			Destination: &opts.KmsID,
		},
		cli.StringFlag{
			Name:        "s3-bucket",
			Usage:       "[REQUIRED] name of the s3-bucket to read from",
			EnvVar:      "BOOT_S3_BUCKET",
			Destination: &opts.S3Bucket,
		},
		cli.StringFlag{
			Name:        "s3-prefix",
			Usage:       "the path prefix for s3",
			EnvVar:      "BOOT_PREFIX",
			Destination: &opts.S3Prefix,
		},
		cli.StringFlag{
			Name:        "dir",
			Usage:       "the local directory to read/write to",
			Value:       ".",
			EnvVar:      "BOOT_DIR",
			Destination: &opts.Dir,
		},
		cli.BoolFlag{
			Name:        "verbose",
			Usage:       "display additional logging",
			EnvVar:      "BOOT_VERBOSE",
			Destination: &opts.Verbose,
		},
		cli.BoolFlag{
			Name:        "dryrun",
			Usage:       "dry run, don't actually make any changes",
			EnvVar:      "BOOT_DRYRUN",
			Destination: &opts.DryRun,
		},
	}

	app := cli.NewApp()
	app.Name = "boot"
	app.Usage = "secret management for containers via S3, IAM, and KMS"
	app.Version = "0.1.0"
	app.Commands = []cli.Command{
		{
			Name:   "container",
			Usage:  "boot the container; should be run from within docker",
			Action: Do(container),
			Flags:  append(flags, logglyFlag),
		},
		{
			Name:   "push",
			Usage:  "push the local directory to the remote directory",
			Action: Do(push),
			Flags:  flags,
		},
		{
			Name:   "pull",
			Usage:  "pull the remote s3 content into the local directory",
			Action: Do(pull),
			Flags:  flags,
		},
	}
	app.Run(os.Args)
}

func Do(fn func(*kms.KMS, *s3.S3, string, ...string) error) func(_ *cli.Context) error {
	return func(c *cli.Context) error {
		root, err := filepath.Abs(opts.Dir)
		if err != nil {
			log.Fatalln(errors.Wrapf(err, "Unable to determine absolute path for dir, %v", opts.Dir))
		}

		cfg := &aws.Config{Region: aws.String(opts.Region)}
		s, err := session.NewSession(cfg)
		if err != nil {
			log.Fatalln(errors.Wrapf(err, "Unable to create aws session for region, %v", opts.Region))
		}

		kmsClient := kms.New(s)
		s3Client := s3.New(s)

		err = fn(kmsClient, s3Client, root, c.Args()...)
		if err != nil {
			log.Fatalln(err)
		}
		return err
	}
}

func s3Key(revision, path string) string {
	return filepath.Join(opts.Env, opts.S3Prefix, revision, path)
}

func filename(revision, root, key string) string {
	prefix := s3Key(revision, "")
	rel := key[len(prefix)+1:]
	return filepath.Join(root, rel)
}

func container(kmsClient *kms.KMS, s3Client *s3.S3, root string, args ...string) error {
	err := pull(kmsClient, s3Client, root)
	if err != nil {
		return errors.Wrap(err, "container:err:pull")
	}

	if len(args) == 0 {
		return errors.New("container:err:args")
	}

	client := loggly.New(opts.LogglyToken, loggly.Interval(time.Second))
	defer client.Flush()

	if opts.Verbose {
		fmt.Println("executing ... ", strings.Join(args, " "))
	}

	w := io.Writer(os.Stdout)

	if opts.LogglyToken != "" {
		w = io.MultiWriter(w, client)
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func push(kmsClient *kms.KMS, s3Client *s3.S3, root string, _ ...string) error {
	walkFunc := uploadFileFunc(kmsClient, s3Client, root)
	err := filepath.Walk(root, walkFunc)
	if err != nil {
		return errors.Wrap(err, "push:err:walk")
	}

	return nil
}

func uploadFileFunc(kmsClient *kms.KMS, s3Client *s3.S3, root string) filepath.WalkFunc {
	return func(path string, info os.FileInfo, err error) error {
		for _, revision := range []string{opts.Revision, time.Now().Format("20060102.1504")} {
			if err != nil {
				return errors.Wrap(err, "upload_file:err:arg")
			}

			if info.IsDir() {
				return nil
			}

			var (
				closer io.Closer
				seeker io.ReadSeeker
			)

			rel := strings.Replace(path, root, "", -1)
			rel = rel[1:]

			key := s3Key(revision, rel)

			if info.Size() > maxEncryptedSize {
				// -- large file, unencrypted -----------------------------
				f, err := os.Open(path)
				if err != nil {
					return errors.Wrapf(err, "upload_file:err:open - %v", path)
					log.Fatalln()
				}

				seeker = f
				closer = f

			} else {
				// -- small file, unencrypted -----------------------------

				data, err := ioutil.ReadFile(path)
				if err != nil {
					return errors.Wrapf(err, "push:err:read_file - %v", path)
				}

				out, err := kmsClient.Encrypt(&kms.EncryptInput{
					KeyId:     aws.String(opts.KmsID),
					Plaintext: data,
				})
				if err != nil {
					return errors.Wrapf(err, "push:err:encrypt - %v", path)
				}

				cipherText := base64.StdEncoding.EncodeToString(out.CiphertextBlob)
				r := strings.NewReader(cipherText)
				seeker = r
				closer = ioutil.NopCloser(r)

				key += EncSuffix
			}

			fmt.Printf("cp %v s3://%v/%v\n", rel, opts.S3Bucket, key)
			_, err = s3Client.PutObject(&s3.PutObjectInput{
				Bucket: aws.String(opts.S3Bucket),
				Key:    aws.String(key),
				Body:   seeker,
			})
			if err != nil {
				return errors.Wrapf(err, "push:err:put_object - %v", path)
			}

			closer.Close()
		}

		return nil
	}
}

// pull mirrors the contents of an S3 dir to the root directory specified
func pull(kmsClient *kms.KMS, s3Client *s3.S3, root string, _ ...string) error {
	prefix := s3Key(opts.Revision, "")

	out, err := s3Client.ListObjects(&s3.ListObjectsInput{
		Bucket: aws.String(opts.S3Bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		return errors.Wrap(err, "pull:err:list_objects")
	}

	for _, item := range out.Contents {
		if strings.HasSuffix(*item.Key, "/") {
			continue
		}

		out, err := s3Client.GetObject(&s3.GetObjectInput{
			Bucket: aws.String(opts.S3Bucket),
			Key:    item.Key,
		})
		if err != nil {
			return errors.Wrapf(err, "pull:err:get_object - %v", *item.Key)
		}

		err = pullFile(kmsClient, root, *item.Key, out.Body)
		if err != nil {
			return errors.Wrapf(err, "pull:err:pull_file - %v", *item.Key)
		}
	}

	return nil
}

// pullFile handles the download (and decryption if necessary) of a single file from S3
func pullFile(kmsClient *kms.KMS, root, key string, r io.ReadCloser) error {
	defer r.Close()

	if strings.HasSuffix(key, EncSuffix) {
		return decryptFile(kmsClient, root, key, r)
	}

	return saveFile(opts.Revision, root, key, r)
}

// saveFile saves the contents of the Reader to a file
func saveFile(revision, root, key string, r io.Reader) error {
	path := filename(revision, root, key)
	os.MkdirAll(filepath.Dir(path), 0755)
	fmt.Printf("saving s3://%v/%v to %v\n", opts.S3Bucket, key, path)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return errors.Wrapf(err, "pull:err:open_file - %v", path)
	}
	defer f.Close()

	_, err = io.Copy(f, r)
	return err
}

// decryptFile decrypts and then saves the contents of the Reader to a file
func decryptFile(kmsClient *kms.KMS, root, key string, r io.Reader) error {
	data, err := ioutil.ReadAll(r)
	if err != nil {
		return errors.Wrapf(err, "decrypt_file:err:read_all - %v", key)
	}

	data, err = base64.StdEncoding.DecodeString(string(data))
	if err != nil {
		return errors.Wrapf(err, "decrypt_file:err:decode_string - %v", key)
	}

	plain, err := kmsClient.Decrypt(&kms.DecryptInput{
		CiphertextBlob: data,
	})
	if err != nil {
		return errors.Wrapf(err, "decrypt_file:err:decrypt - %v", key)
	}

	path := filename(opts.Revision, root, key)
	path = strings.Replace(path, EncSuffix, "", -1)

	os.MkdirAll(filepath.Dir(path), 0755)
	fmt.Printf("saving s3://%v/%v to %v\n", opts.S3Bucket, key, path)
	if opts.DryRun {
		return nil
	}

	if opts.File != "" && strings.HasSuffix(path, opts.File) && path == filepath.Join(root, opts.File) {
		err := loadEnv(bytes.NewReader(plain.Plaintext))
		if err != nil {
			return errors.Wrap(err, "decrypt_file:err:load_env")
		}
		return nil
	}

	ioutil.WriteFile(path, plain.Plaintext, 0644)
	return nil
}

func loadEnv(r io.Reader) error {
	buf := bufio.NewReader(r)
	lineNum := 0

	for {
		lineNum++
		v, _, err := buf.ReadLine()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return errors.Wrapf(err, "Unable to parse line %v", lineNum)
		}

		line := strings.TrimSpace(string(v))
		if strings.HasPrefix(line, "#") || strings.HasPrefix(line, "/") {
			continue
		}

		segments := strings.SplitN(line, "=", 2)
		if len(segments) != 2 {
			continue
		}

		key := strings.TrimSpace(segments[0])
		value := strings.TrimSpace(segments[1])

		if key != "" && value != "" {
			os.Setenv(key, value)
		}
	}
}
