package utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	s3Types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
	"go.mau.fi/whatsmeow"
)

func downloadS3Object(ctx context.Context, s3Client *s3.Client, bucketName string, objectKey string) (audioBytes []byte, err error) {
	// TODO: Implement S3 object download
	// https://docs.aws.amazon.com/code-library/latest/ug/go_2_s3_code_examples.html#:r5d:-trigger
	
	result, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(objectKey),
	})
	if err != nil {
		var noKey *types.NoSuchKey
		if errors.As(err, &noKey) {
			log.Printf("Can't get object %s from bucket %s. No such key exists.\n", objectKey, bucketName)
			err = noKey
		} else {
			log.Printf("Couldn't get object %v:%v. Here's why: %v\n", bucketName, objectKey, err)
		}
		return nil, err
	}
	defer result.Body.Close()
	
	body, err := io.ReadAll(result.Body)
	if err != nil {
		log.Printf("Couldn't read object body from %v. Here's why: %v\n", objectKey, err)
	}
	return body, err
}

func uploadToS3(ctx context.Context, s3Client *s3.Client, bucketName string, objectKey string, mediaData []byte) error {
	reader := bytes.NewReader(mediaData)

	input := &s3.ListObjectsV2Input{
		Bucket: aws.String(bucketName),
	}
	
	objectPaginator := s3.NewListObjectsV2Paginator(s3Client, input)
	for objectPaginator.HasMorePages() {
		output, err := objectPaginator.NextPage(ctx)
		if err != nil {
			var noBucket *s3Types.NoSuchBucket
			if errors.As(err, &noBucket) {
				log.Printf("Bucket %s does not exist.\n", bucketName)
				return noBucket
			}
			break
		} else {
			for _, object := range output.Contents {
				if *object.Key == objectKey {
					// Return early if object already exists
					log.Printf("Object %s already exists in bucket %s. Skipping upload.\n", objectKey, bucketName)
					return fmt.Errorf("object %s already exists", objectKey)
				}
			}
		}
	}

	_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: &bucketName,
		Key:    &objectKey,
		Body:   reader,
	})

	if err != nil {
		var apiErr smithy.APIError
		if errors.As(err, &apiErr) && apiErr.ErrorCode() == "EntityTooLarge" {
			log.Printf("Error while uploading object to %s. The object is too large.\n"+
				"To upload objects larger than 5GB, use the S3 console (160GB max)\n"+
				"or the multipart upload API (5TB max).", bucketName)
		} else {
			log.Printf("Couldn't upload file to %v:%v. Here's why: %v\n",
				bucketName, objectKey, err)
		}
	}

	err = s3.NewObjectExistsWaiter(s3Client).Wait(
		ctx, &s3.HeadObjectInput{Bucket: aws.String(bucketName), Key: aws.String(objectKey)}, time.Minute)
	if err != nil {
		log.Printf("Failed attempt to wait for object %s to exist.\n", objectKey)
	}

	return err
}

// Handle S3 upload for a WhatsApp message (text or media).
// If both content and mediaType are provided, media upload takes precedence
func uploadMessageToS3(client *whatsmeow.Client, s3Client *s3.Client, bucketName string, content string, messageID string, chatJID string, mediaType string, filename string, url string, mediaKey []byte, fileSHA256 []byte, fileEncSHA256 []byte, fileLength uint64) (filePath string, err error) {
	// initialize variables
	var mediaData []byte

	if content == "" && mediaType == "" {
		return "", fmt.Errorf("no content or media to upload for message %s", messageID)
	}

	if content != "" {
		mediaData = []byte(content)
	}
	
	if mediaType != "" {
		mediaData, err = downloadWhatsAppMedia(client, messageID, chatJID, mediaType, url, mediaKey, fileSHA256, fileEncSHA256, fileLength)
		if err != nil {
			return "", fmt.Errorf("failed to download media for S3 upload: %v", err)
		}
	} 

	objectKey := fmt.Sprintf("input/%s/%s", chatJID, filename)

	// upload to S3
	err = uploadToS3(context.Background(), s3Client, bucketName, objectKey, mediaData)
	if err != nil {
		return "", fmt.Errorf("failed to upload media to S3: %v", err)
	}

	return fmt.Sprintf("%s/%s", bucketName, objectKey), nil
}