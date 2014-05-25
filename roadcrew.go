package main
// roadcrew.go: Run Sysdig and upload to S3 continuously

import (
	"github.com/docopt/docopt-go"
	"io/ioutil"
	"launchpad.net/goamz/aws"
	"launchpad.net/goamz/s3"
	"log"
	"mime"
	"errors"
	"os"
	"os/exec"
	"path"
	"time"
)

var usage string = `roadcrew

Usage:
  roadcrew [options] -i <interval> -b <bucket_name>
  roadcrew -h --help
  roadcrew --version

Options:
  -K <aws_key_id>       AWS Key ID.
  -S <aws_secret_key>   AWS Secret Key.
  -r <aws_region>       AWS Region [default: us-east-1].
  -t <tmp_dir>          Specify tmp directory.
  -h, --help            Show this screen.
  --version             Show version.

Environment variables: either set these or use -S and -K
  export AWS_ACCESS_KEY_ID="AAAAAAAAAAAAAAAAAAAAA"
  export AWS_SECRET_ACCESS_KEY="BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB"
`

func main() {
  opts, err := setupOptions()
  if err != nil {
    log.Fatal(err)
  }

  // Kick off initial command
	sysdigreturn := make(chan string)
	go runSysdig(sysdigreturn, opts)

  // Handle command results, launch additional commands
	for {
		select {
		case filename := <-sysdigreturn:
      go runSysdig(sysdigreturn, opts)
      go handleTraceFile(filename, opts)
		}
	}
}

// Run the Sysdig command - when done, return the sysdig tracefile name over the sysdigreturn channel
func runSysdig(sysdigreturn chan string, opts roadcrewOptions) {

	tracefile, err := ioutil.TempFile(opts.tmpDir, "rc_sysdig_")
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Starting sysdig run")
	cmd := exec.Command("sysdig", "-qw", tracefile.Name())
  if err := cmd.Start(); err != nil {
    log.Fatal(err)
  }

  done := make(chan error)
  go func() {
    done <- cmd.Wait()
  }()

  select {
    case err := <-done:
      log.Println("Sysdig exited before we expected it to!")
      if err != nil {
        log.Fatal(err)
      }
    case <-time.After( opts.timeout ):
      if err := cmd.Process.Signal(os.Interrupt); err != nil {
        log.Fatal("Failed to kill: ", err)
      }
      <-done // allow goroutine to exit
      // log.Printf("Sysdig killed after %s seconds", interval)
  }

	log.Println("Finished sysdig run")
	sysdigreturn <- tracefile.Name()
}

// Do work after the trace file is ready
func handleTraceFile(filename string, opts roadcrewOptions) error {
  uploadFinished, statsFinished := false, false
  uploadChan := make(chan string)
  statsChan := make(chan string)

	go upload(uploadChan, filename, opts.bucketName, opts.auth, opts.region)
	go getTraceStats(statsChan, filename)

  for uploadFinished==false || statsFinished==false {
    select {
    case <-uploadChan:
      uploadFinished=true 
    case <-statsChan:
      statsFinished=true
    case <- time.After( opts.timeout ):
      log.Fatal("Upload timed out - TODO: handle this more cleanly")
    }
  }
  return nil
}

// Upload the file to S3 - much credit to https://launchpad.net/s3up
func upload(uploadChan chan string, local, bucketname string, auth aws.Auth, region aws.Region) error {
	log.Printf("Starting upload of tracefile %s\n", local)

  // Open/stat the local file
	localf, err := os.Open(local)
	if err != nil {
		log.Fatal(err)
	}
	defer localf.Close()
	localfi, err := localf.Stat()
	if err != nil {
		log.Fatal(err)
	}

  // Create a remote (S3) directory/filename
	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}
	remote := hostname + "/" + path.Base(local)

  // File permissions in S3
	acl := s3.Private

  // File content type
	contType := mime.TypeByExtension(path.Ext(local))
	if contType == "" {
		contType = "binary/octet-stream"
	}

  // File content type
	bucket := s3.New(auth, region).Bucket(bucketname)

  // Use single-part upload for small files, multipart for big ones
	if localfi.Size() <= 5*MB {
		err = bucket.PutReader(remote, &progressFile{File: localf}, localfi.Size(), contType, acl)
		if err != nil {
      log.Fatal(err)
		}
    // Remove the local file
		err = os.Remove(local)
		if err != nil {
      log.Fatal(err)
		}
    uploadChan <- "upload done"
		log.Printf("Finished upload of tracefile %s\n", local)
    return err
	}
	multi, err := bucket.Multi(remote, contType, acl)
	if err != nil {
		log.Fatal(err)
	}
	parts, err := multi.PutAll(&progressFile{File: localf}, 5*MB)
	if err != nil {
		log.Fatal(err)
	}

	err = multi.Complete(parts)
	if err != nil {
		log.Fatal(err)
	}
  // Remove the local file
	err = os.Remove(local)
	if err != nil {
		log.Fatal(err)
	}
  uploadChan <- "upload done"
	log.Printf("Finished multipart upload of tracefile %s\n", local)
	return err
}

type progressFile struct {
	*os.File
	n int
}

func (pr *progressFile) Read(b []byte) (n int, err error) {
	n, err = pr.File.Read(b)
	return
}

func (pr *progressFile) ReadAt(b []byte, off int64) (n int, err error) {
	n, err = pr.File.ReadAt(b, off)
	return
}

// Keep an eye on how much load we're adding
func getTraceStats(statsChan chan string, filename string) {
	cmd := exec.Command("sysdig", "-q", "-c", "topprocs_cpu", "-r", filename)
  output, err := cmd.Output()
  if err != nil {
    log.Fatal(err)
  }
  log.Println(string(output))

	cmd = exec.Command("sysdig", "-q", "-c", "topprocs_net", "-r", filename)
  output, err = cmd.Output()
  if err != nil {
    log.Fatal(err)
  }
  log.Println(string(output))

  statsChan <- "getTraceStats done"
}

// Parse and validate our options
func setupOptions() (roadcrewOptions, error)  {
  opts := roadcrewOptions{}

  // Handle command line options
	arguments, err := docopt.Parse(usage, nil, true, "roadcrew 0.1", false)
	if err != nil {
		return opts, err
	}

	opts.interval = arguments["<interval>"].(string)
  opts.timeout, err = time.ParseDuration(opts.interval+"s") 
  if err != nil {
    log.Fatal( err )
  }

  opts.bucketName = arguments["<bucket_name>"].(string)
  opts.tmpDir = "/tmp"
  if arguments["--tmp_dir"] != nil {
    opts.tmpDir = arguments["--tmp_dir"].(string)
  }

  // Check if valid AWS region
  if _, present := aws.Regions[arguments["-r"].(string)]; present == false {
    log.Fatalf("Invalid AWS region: %s", arguments["-r"])
  }
	opts.region = aws.Regions[arguments["-r"].(string)]

  // Read AWS credentials from the environment
  opts.auth, err = aws.EnvAuth()
  if err != nil {
    // Read AWS credentials from the CLI if not in env
    if arguments["-K"] == nil || arguments["-S"] == nil {
      return opts, errors.New("AWS credentials not found: must use -K and -S flags, or set these env vars:\n\texport AWS_ACCESS_KEY_ID=\"AAA...\"\n\texport AWS_SECRET_ACCESS_KEY=\"BBBB...\"\n")
    }
    opts.auth = aws.Auth{
      AccessKey: arguments["-K"].(string),
      SecretKey: arguments["-S"].(string),
    }
  }

  if err := checkDependencies(opts); err != nil {
    return opts, err
  }

  return opts, nil
}

// Validate other dependencies
func checkDependencies(opts roadcrewOptions) error {
  if _, err := exec.LookPath("sysdig"); err != nil {
    return errors.New("Couldn't find the sysdig command in your path... install with this command:\n\tcurl -s https://s3.amazonaws.com/download.draios.com/stable/install-sysdig | sudo bash\n")
  }
  if _, err := os.Stat(opts.tmpDir); err != nil {
    return err
  }
  if euid := os.Geteuid(); euid != 0 {
    return errors.New("Must run roadcrew/sysdig as root user")
  }
	bucket := s3.New(opts.auth, opts.region).Bucket(opts.bucketName)
  if _, err := bucket.List("/", "/", "", 1); err != nil {
    return err
  }
  return nil
}

// Constants
const MB = 1024 * 1024

// Container for our options
type roadcrewOptions struct {
  auth aws.Auth
  region aws.Region
	interval string
	timeout time.Duration
  bucketName string
  tmpDir string
}

