package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2Types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"log"
	"os"
	"sync"
)

func main() {
	f := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	dryRun := f.Bool("dry-run", false, "When specified, show which AMIs would be deregistered without actually deregistering them.")
	//verbose := f.Bool("verbose", false, "When specified, explicitly states which regions are not at their quota.")
	if err := f.Parse(os.Args[1:]); err != nil {
		panic(err)
	}

	cfg, err := config.LoadDefaultConfig(context.TODO())
	if err != nil {
		log.Fatalf("unable to load SDK config, %v", err)
	}

	ec2RegionClient := ec2.NewFromConfig(cfg)
	enabledRegions, err := getEnabledRegions(ec2RegionClient)
	if err != nil {
		log.Fatalf("error fetching enabled regions for AWS account: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(len(enabledRegions))

	for _, enabledRegion := range enabledRegions {
		go func(regionName string) {
			defer wg.Done()

			ec2Client := ec2.NewFromConfig(cfg, func(o *ec2.Options) { o.Region = regionName })
			// Create CreateTagsInputClient adapter

			images, err := getPublicImages(ec2Client)
			if err != nil {
				log.Fatalf("error fetching images for region %v: %v", regionName, err)
			}
			images = filterImages(images)

			if !*dryRun {
				createTagsClient := ec2.NewFromConfig(cfg, func(o *ec2.Options) { o.Region = regionName })

				err = addTagToImages(images, "version", "legacy-x86_64", createTagsClient, regionName)
				if err != nil {
					fmt.Println("Error adding tags:", err)
				}
			} else {
				for _, img := range images {
					fmt.Printf("%v: Would of Tagged image %s with tag '%s' = '%s'\n", regionName, *img.ImageId, "version", "legacy-x86_64")
				}
			}

		}(*enabledRegion.RegionName)
	}

	wg.Wait()
	fmt.Println("Done!")
}

func addTagToImages(images []ec2Types.Image, tagName string, tagValue string, createTagsClient *ec2.Client, region string) error {

	for _, img := range images {
		input := &ec2.CreateTagsInput{
			//DryRun:    aws.Bool(true),
			Resources: []string{*img.ImageId},
			Tags: []ec2Types.Tag{
				{
					Key:   aws.String(tagName),
					Value: aws.String(tagValue),
				},
			},
		}
		_, err := createTagsClient.CreateTags(context.TODO(), input)
		if err != nil {
			return fmt.Errorf("failed to tag image %s: %v", *img.ImageId, err)
		}

		fmt.Printf("%v: Tagged image %s with tag '%s' = '%s'\n", region, *img.ImageId, tagName, tagValue)
	}

	return nil
}

// getEnabledRegions returns all enabled regions
func getEnabledRegions(describeRegionsClient DescribeRegionsClient) ([]ec2Types.Region, error) {
	describeRegionsResponse, err := describeRegionsClient.DescribeRegions(context.TODO(), nil)
	if err != nil {
		return nil, fmt.Errorf("error fetching regions: %w", err)
	}
	return describeRegionsResponse.Regions, nil
}

// getPublicImages retrieves all public images belonging to the AWS account
func getPublicImages(ec2Client DescribeImagesClient) ([]ec2Types.Image, error) {
	describeImagesResponse, err := ec2Client.DescribeImages(context.TODO(), &ec2.DescribeImagesInput{
		ExecutableUsers: []string{"all"},
		Owners:          []string{"self"},
	})
	if err != nil {
		return nil, fmt.Errorf("error fetching images: %w", err)
	}
	return describeImagesResponse.Images, nil
}

func lacksVersionTag(image ec2Types.Image) bool {
	for _, t := range image.Tags {
		if *t.Key == "version" {
			return false // Found the version tag, so it does not lack the tag
		}
	}
	return true // Did not find the version tag, so it lacks the tag
}

// filterImages filters AWS AMIs based on their version tag and architecture.
func filterImages(images []ec2Types.Image) []ec2Types.Image {
	var imagesNoTag []ec2Types.Image
	for _, image := range images {

		if lacksVersionTag(image) {
			imagesNoTag = append(imagesNoTag, image)
		}
	}
	return imagesNoTag
}
