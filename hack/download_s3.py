#!/usr/bin/env python3
"""Download one S3 prefix with nothing but the Python standard library.

The training image has no curl, no aws-cli and no boto3, and pods have no PyPI
egress, so SigV4 is implemented here directly. Header auth is used for both
ListObjectsV2 and GetObject.

Env:
    S3_ACCESS_KEY_ID, S3_SECRET_ACCESS_KEY   (from the platform Secret)
    S3_ENDPOINT_URL   default http://internal.s3-cn-sh2.ufileos.com
    S3_REGION_NAME    default cn-sh2

Usage: download_s3.py <bucket> <prefix> <dest-dir>
"""

from __future__ import annotations

import datetime
import hashlib
import hmac
import os
import sys
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

EMPTY_SHA256 = hashlib.sha256(b"").hexdigest()


def env(name: str, default: str) -> str:
    return os.environ.get(name, default).strip()


def sign_headers(method: str, canonical_uri: str, query: dict[str, str], endpoint: str, region: str,
                 access: str, secret: str) -> dict[str, str]:
    host = urllib.parse.urlparse(endpoint).netloc
    now = datetime.datetime.now(datetime.timezone.utc)
    amz_date = now.strftime("%Y%m%dT%H%M%SZ")
    date_stamp = now.strftime("%Y%m%d")

    items = sorted(query.items())
    canonical_query = "&".join(
        f"{urllib.parse.quote(k, safe='-_.~')}={urllib.parse.quote(v, safe='-_.~')}"
        for k, v in items
    )
    canonical_headers = (
        f"host:{host}\n"
        f"x-amz-content-sha256:{EMPTY_SHA256}\n"
        f"x-amz-date:{amz_date}\n"
    )
    signed_headers = "host;x-amz-content-sha256;x-amz-date"
    canonical_request = "\n".join(
        [method, canonical_uri, canonical_query, canonical_headers, signed_headers, EMPTY_SHA256]
    )
    scope = f"{date_stamp}/{region}/s3/aws4_request"
    string_to_sign = "\n".join([
        "AWS4-HMAC-SHA256",
        amz_date,
        scope,
        hashlib.sha256(canonical_request.encode()).hexdigest(),
    ])
    key = hmac.new(("AWS4" + secret).encode(), date_stamp.encode(), hashlib.sha256).digest()
    key = hmac.new(key, region.encode(), hashlib.sha256).digest()
    key = hmac.new(key, b"s3", hashlib.sha256).digest()
    key = hmac.new(key, b"aws4_request", hashlib.sha256).digest()
    signature = hmac.new(key, string_to_sign.encode(), hashlib.sha256).hexdigest()
    return {
        "Authorization": (
            f"AWS4-HMAC-SHA256 Credential={access}/{scope}, "
            f"SignedHeaders={signed_headers}, Signature={signature}"
        ),
        "x-amz-content-sha256": EMPTY_SHA256,
        "x-amz-date": amz_date,
    }


def build_url(endpoint: str, canonical_uri: str, query: dict[str, str]) -> str:
    url = endpoint.rstrip("/") + canonical_uri
    if query:
        url += "?" + urllib.parse.urlencode(query)
    return url


def list_buckets(*, endpoint, region, access, secret):
    headers = sign_headers("GET", "/", {}, endpoint, region, access, secret)
    request = urllib.request.Request(endpoint.rstrip("/") + "/", headers=headers)
    with urllib.request.urlopen(request, timeout=30) as response:
        body = response.read()
    root = ET.fromstring(body)
    names = []
    for bucket in root.iter():
        if bucket.tag.endswith("Name") and bucket.text:
            names.append(bucket.text)
    return names


def _local(tag: str) -> str:
    return tag.rsplit("}", 1)[-1]


def list_objects(*, endpoint, region, access, secret, bucket, prefix):
    """List one prefix. US3 returns S3 XML without the AWS namespace, so the
    parser matches local tag names instead of a qualified path."""
    keys = []
    token = ""
    while True:
        query = {"list-type": "2", "prefix": prefix, "max-keys": "1000"}
        if token:
            query["continuation-token"] = token
        canonical_uri = "/" + bucket
        headers = sign_headers("GET", canonical_uri, query, endpoint, region, access, secret)
        request = urllib.request.Request(build_url(endpoint, canonical_uri, query), headers=headers)
        with urllib.request.urlopen(request, timeout=30) as response:
            body = response.read()
        root = ET.fromstring(body)
        for content in root:
            if _local(content.tag) != "Contents":
                continue
            key, size = "", 0
            for field in content:
                name = _local(field.tag)
                if name == "Key":
                    key = field.text or ""
                elif name == "Size":
                    size = int(field.text or 0)
            if key and not key.endswith("/"):
                keys.append((key, size))
        truncated, next_token = "false", ""
        for field in root:
            name = _local(field.tag)
            if name == "IsTruncated":
                truncated = (field.text or "false").lower()
            elif name == "NextContinuationToken":
                next_token = field.text or ""
        if truncated != "true" or not next_token:
            return keys
        token = next_token


def download(*, endpoint, region, access, secret, bucket, key, path):
    canonical_uri = "/" + bucket + "/" + urllib.parse.quote(key, safe="/-_.~")
    headers = sign_headers("GET", canonical_uri, {}, endpoint, region, access, secret)
    request = urllib.request.Request(build_url(endpoint, canonical_uri, {}), headers=headers)
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with urllib.request.urlopen(request, timeout=120) as response, open(path, "wb") as handle:
        while True:
            chunk = response.read(1 << 20)
            if not chunk:
                break
            handle.write(chunk)


def main() -> int:
    if len(sys.argv) == 2 and sys.argv[1] == "--buckets":
        endpoint = env("S3_ENDPOINT_URL", "http://internal.s3-cn-sh2.ufileos.com")
        region = env("S3_REGION_NAME", "cn-sh2")
        access = os.environ.get("S3_ACCESS_KEY_ID", "")
        secret = os.environ.get("S3_SECRET_ACCESS_KEY", "")
        try:
            names = list_buckets(endpoint=endpoint, region=region, access=access, secret=secret)
            print(f"buckets: {names}")
        except urllib.error.HTTPError as error:
            print(f"HTTP {error.code}: {error.read()[:400]!r}")
        return 0
    if len(sys.argv) == 3 and sys.argv[1] == "--probe":
        endpoint = env("S3_ENDPOINT_URL", "http://internal.s3-cn-sh2.ufileos.com")
        region = env("S3_REGION_NAME", "cn-sh2")
        access = os.environ.get("S3_ACCESS_KEY_ID", "")
        secret = os.environ.get("S3_SECRET_ACCESS_KEY", "")
        bucket = sys.argv[2]
        for prefix in ("", "datasets/", "datasets/medmcqa/", "datasets/medmcqa/2.0.1/", "models/"):
            keys = list_objects(endpoint=endpoint, region=region, access=access, secret=secret,
                                bucket=bucket, prefix=prefix)
            print(f"prefix={prefix!r} count={len(keys)}")
            for key, size in keys[:20]:
                print(f"  {key}  {size}")
        return 0
    if len(sys.argv) == 4 and sys.argv[1] == "--list":
        endpoint = env("S3_ENDPOINT_URL", "http://internal.s3-cn-sh2.ufileos.com")
        region = env("S3_REGION_NAME", "cn-sh2")
        access = os.environ.get("S3_ACCESS_KEY_ID", "")
        secret = os.environ.get("S3_SECRET_ACCESS_KEY", "")
        bucket, prefix = sys.argv[2], sys.argv[3]
        keys = list_objects(endpoint=endpoint, region=region, access=access, secret=secret,
                            bucket=bucket, prefix=prefix)
        print(f"prefix={prefix!r} count={len(keys)}")
        for key, size in keys[:40]:
            print(f"  {key}  {size}")
        return 0
    if len(sys.argv) != 4:
        print(__doc__)
        return 2
    bucket, prefix, dest = sys.argv[1], sys.argv[2].strip("/"), sys.argv[3]
    endpoint = env("S3_ENDPOINT_URL", "http://internal.s3-cn-sh2.ufileos.com")
    region = env("S3_REGION_NAME", "cn-sh2")
    access = os.environ.get("S3_ACCESS_KEY_ID", "")
    secret = os.environ.get("S3_SECRET_ACCESS_KEY", "")
    if not access or not secret:
        print("S3 credentials missing from the environment")
        return 1

    print(f"listing s3://{bucket}/{prefix} via {endpoint}")
    keys = list_objects(endpoint=endpoint, region=region, access=access, secret=secret,
                        bucket=bucket, prefix=prefix + "/")
    if not keys:
        print("no objects under the prefix")
        return 3
    print(f"{len(keys)} object(s), {sum(size for _, size in keys)} bytes")

    for key, size in keys:
        relative = key[len(prefix) + 1:]
        target = os.path.join(dest, relative)
        if os.path.exists(target) and os.path.getsize(target) == size:
            print(f"  skip {relative} ({size} B)")
            continue
        print(f"  get  {relative} ({size} B)")
        download(endpoint=endpoint, region=region, access=access, secret=secret,
                 bucket=bucket, key=key, path=target)
    print(f"done -> {dest}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
