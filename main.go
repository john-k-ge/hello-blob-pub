package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"bytes"

	"github.build.ge.com/212419672/cf-service-tester/cfServiceDiscovery"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/cloudfoundry-community/go-cfenv"
)

var myService cfServiceDiscovery.ServiceDescriptor
var keyId, keySecret, bucketUrl, bucketName, hostName string
var awsConfig aws.Config
var s3Svc *s3.S3

const access_key_id = "access_key_id"
const secret_access_key = "secret_access_key"
const bucket_url = "url"
const bucket_name = "bucket_name"
const host = "host"
const inFileName = "tasmanian_devil.png"
const outFileName = "outfile.png"
const contentType = "image/png"

func serviceDescriptor(w http.ResponseWriter, req *http.Request) {
	data, err := json.Marshal(&myService)
	if err != nil {
		fmt.Printf("Cannot generate service descriptor: %v", err)
		fmt.Fprintf(w, "Cannot generate service descriptor: %v", err)
		return
	}
	fmt.Printf("Here's the data:  %s", data)
	json.NewEncoder(w).Encode(myService)

	return
}

func testBlob(w http.ResponseWriter, req *http.Request) {
	if len(hostName) == 0 {
		fmt.Fprint(w, "`Sorry, but I'm not bound to a Blob instance.  Please bind me!`\n")
		return
	}
	imageIn, err := os.Open(inFileName)
	defer imageIn.Close()

	imageOut, err := os.Create(outFileName)

	if err != nil {
		log.Printf("Could not open local files %v, %v", inFileName, outFileName)
		log.Printf("Error: %v\n", err.Error())
		fmt.Fprintf(w, "Sorry, I could not open my test file to upload: %v, %v\n", inFileName, outFileName)
		fmt.Fprintf(w, "Error: %v\n", err.Error())
		fmt.Fprint(w, "I can't test blob store until this is fixed :(\n")
		return
	}

	inFileInfo, err := imageIn.Stat()
	if err != nil {
		log.Printf("Could not calculate the size of the file %v", inFileName)
		log.Printf("Error: %v\n", err.Error())
		fmt.Fprintf(w, "Sorry, I could not calculate the size of the file: %v\n", inFileName)
		fmt.Fprintf(w, "Error: %v\n", err.Error())
		fmt.Fprint(w, "I can't test blob store until this is fixed :(\n")
		return
	}
	inputFileSize := inFileInfo.Size()
	log.Printf("Input file %v is %v bytes", inFileName, inputFileSize)

	buffer := make([]byte, inputFileSize)

	imageIn.Read(buffer)

	fileBytes := bytes.NewReader(buffer)

	fileContentType := http.DetectContentType(buffer)

	_, err = s3Svc.PutObject(&s3.PutObjectInput{
		Body:          fileBytes,
		Bucket:        aws.String(bucketName),
		Key:           aws.String(inFileName),
		ContentType:   aws.String(fileContentType),
		ContentLength: aws.Int64(inputFileSize),
	})

	if err != nil {
		log.Printf("I could not upload my file to the blobstore: %v", err.Error())
		fmt.Fprintf(w, "I could not upload my file to the blobstore: %v", err.Error())
		return
	}

	fmt.Fprintf(w, "I uploaded my file to the blobstore: %v\n", inFileName)

	time.Sleep(2 * time.Second)

	resp, err := s3Svc.GetObject(&s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(inFileName),
	})

	if err != nil {
		log.Printf("I could not download my file from the blobstore: %v", err.Error())
		fmt.Fprintf(w, "I could not download my file from the blobstore: %v", err.Error())
		return
	}

	defer resp.Body.Close()

	io.Copy(imageOut, resp.Body)
	defer os.Remove(outFileName)

	outFileInfo, err := imageOut.Stat()
	if err != nil {
		log.Printf("Could not calculate the size of the file %v", outFileName)
		log.Printf("Error: %v\n", err.Error())
		fmt.Fprintf(w, "Sorry, I could not calculate the size of the file: %v\n", outFileName)
		fmt.Fprintf(w, "Error: %v\n", err.Error())
		fmt.Fprint(w, "I can't test blob store until this is fixed :(\n")
		return
	}
	outputFileSize := outFileInfo.Size()

	switch {
	case inputFileSize == outputFileSize:
		log.Printf("File sizes match: %v bytes in, and %v bytes out!!", inputFileSize, outputFileSize)
		fmt.Fprintf(w, "File sizes match: %v bytes in, and %v bytes out!!\n", inputFileSize, outputFileSize)
	case inputFileSize > outputFileSize:
		log.Printf("File sizes do not match: in: %v, out: %v", inputFileSize, outputFileSize)
		fmt.Fprint(w, "Downloaded file is too small!\n")
	default:
		log.Printf("File sizes do not match: in: %v, out: %v", inputFileSize, outputFileSize)
		fmt.Fprint(w, "Downloaded file is too big!\n")
	}

	log.Print("About to delete the image from Blob")

	_, err = s3Svc.DeleteObject(&s3.DeleteObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(inFileName),
	})

	if err != nil {
		log.Printf("I could not delete my file from the blobstore: %v", err.Error())
		fmt.Fprintf(w, "I could not delete my file from the blobstore: %v", err.Error())
		return
	}

	log.Printf("Deleted %v from Blob successfully", inFileName)
	fmt.Fprintf(w, "Deleted %v from Blob successfully!!\n", inFileName)
	fmt.Fprint(w, ":tada: Everything is fine!!")
}

func init() {
	blobServiceLabel := os.Getenv("SERVICE_NAME")
	fmt.Printf("BlobServiceLabel = %v\n", blobServiceLabel)

	appEnv, _ := cfenv.Current()

	services := appEnv.Services
	if len(services) > 0 {
		blobServices, err := services.WithLabel(blobServiceLabel)
		if err != nil || len(blobServices) < 1 {
			panic("No " + blobServiceLabel + " service found!!")
		}

		for _, service := range blobServices {
			fmt.Printf("name: %v, val: %v\n", service.Name, service.Credentials)
			fmt.Printf("url: %v\n", service.Credentials["url"])
		}

		blobService := blobServices[0]

		keyId = blobService.Credentials[access_key_id].(string)
		keySecret = blobService.Credentials[secret_access_key].(string)
		bucketUrl = blobService.Credentials[bucket_url].(string)
		bucketName = blobService.Credentials[bucket_name].(string)
		hostName = blobService.Credentials[host].(string)

		//region := "us-west-2"
		region := os.Getenv("REGION")
		disableSSL := true
		logLevel := aws.LogDebugWithRequestErrors

		awsConfig = aws.Config{
			Credentials: credentials.NewStaticCredentials(keyId, keySecret, ""),
			Region:      &region,
			Endpoint:    &hostName,
			DisableSSL:  &disableSSL,
			LogLevel:    &logLevel,
		}

		s := session.New(&awsConfig)
		s3Svc = s3.New(s)
	}

	myService = cfServiceDiscovery.ServiceDescriptor{
		AppName:     appEnv.Name,
		AppUri:      appEnv.ApplicationURIs[0],
		ServiceName: os.Getenv("SERVICE_NAME"),
		PlanName:    os.Getenv("SERVICE_PLAN"),
	}
}

func main() {
	fmt.Println("Starting...")
	port := os.Getenv("PORT")
	log.Printf("Listening on port %v", port)
	if len(port) == 0 {
		port = "9000"
	}

	http.HandleFunc("/info", serviceDescriptor)
	http.HandleFunc("/ping", testBlob)
	err := http.ListenAndServe(":"+port, nil)
	if err != nil {
		log.Printf("ListenAndServe: %v", err)
	}

}
