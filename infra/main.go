package main

import (
	"github.com/pulumi/pulumi-cloudflare/sdk/v5/go/cloudflare"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi"
	"github.com/pulumi/pulumi/sdk/v3/go/pulumi/config"
)

func main() {
	pulumi.Run(func(ctx *pulumi.Context) error {
		cfg := config.New(ctx, "")
		accountID := cfg.Require("accountId")

		bucket, err := cloudflare.NewR2Bucket(ctx, "solar-data", &cloudflare.R2BucketArgs{
			AccountId: pulumi.String(accountID),
			Name:      pulumi.String("solar-data"),
			Location:  pulumi.String("WEUR"), // Western Europe — closest to UK
		})
		if err != nil {
			return err
		}

		ctx.Export("bucketName", bucket.Name)


		return nil
	})
}
