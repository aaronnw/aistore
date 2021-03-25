// Package s3compat provides Amazon S3 compatibility layer
/*
 * Copyright (c) 2018-2020, NVIDIA CORPORATION. All rights reserved.
 */
package s3compat

const (
	AISRegion = "ais"
	AISSever  = "AIS"

	// versioning
	URLParamVersioning  = "versioning" // URL parameter
	URLParamMultiDelete = "delete"
	versioningEnabled   = "Enabled"
	versioningDisabled  = "Suspended"

	s3Namespace = "http://s3.amazonaws.com/doc/2006-03-01"
	// TODO: can it be omitted? // storageClass = "STANDARD"

	// Headers
	headerETag    = "ETag"
	headerVersion = "x-amz-version-id"
	HeaderObjSrc  = "x-amz-copy-source"

	headerAtime = "Last-Modified"
)
