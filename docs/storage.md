---
title: "Storage"
description: "Object storage organized into buckets that hold objects (files). Policies govern access to a bucket, and each object has a public/private visibility."
---

## What it is

Object storage organized into **buckets** that hold **objects** (files).
**Policies** govern access to a bucket, and each object has a public/private
**visibility**.

## How it relates

- Belongs to a **project**.
- **Functions** read and write objects at runtime.
- **Policies** control access; they can be managed with the CLI or declared in
  the [declarative config](project-configuration.md) (an omitted `policies`
  key leaves a bucket's policies untouched).

## CLI operations

| Element | CLI can… | Command |
|---|---|---|
| Bucket | create, list, get, update, delete | `volcano storage bucket create\|list\|get\|update\|delete …` |
| Object | list, upload, download, copy, move, delete, set visibility | `volcano storage object list\|upload\|download\|copy\|move\|delete\|visibility …` |
| Policy | attach, list, get, delete | `volcano storage policy create\|list\|get\|delete …` |
| Usage | aggregate stats | `volcano storage stats` |

Prefix with `cloud` to force the cloud target.

## Examples

```bash
# Create a bucket
volcano storage bucket create uploads

# Upload / download objects (download - writes to stdout)
volcano storage object upload uploads ./photo.png images/photo.png
volcano storage object download uploads images/photo.png ./photo.png
volcano storage object download uploads images/photo.png -

# List, move, delete, and change visibility
volcano storage object list uploads
volcano storage object move uploads images/photo.png images/avatar.png
volcano storage object visibility uploads images/avatar.png public
volcano storage object delete uploads images/avatar.png

# Attach a policy and see usage
volcano storage policy create uploads --name public-read
volcano storage stats
```
