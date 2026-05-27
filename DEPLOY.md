# Deploying bgpicker to AWS

Two options are provided. **Lambda + S3 is recommended** — it costs essentially
nothing for a small group of friends. EC2 is simpler to reason about but charges
a small monthly fee even when the app is idle.

| Option | Cost |
|---|---|
| Lambda + S3 | ~$0/month (well within the perpetual free tier) |
| EC2 t4g.nano (always-on) | ~$3.50/month |
| EC2 t4g.nano (stop between game nights) | ~$0.85/month |

---

## Prerequisites

- [AWS CLI v2](https://docs.aws.amazon.com/cli/latest/userguide/getting-started-install.html) installed
- Credentials configured: run `aws configure` and enter your Access Key ID,
  Secret Access Key, default region (e.g. `eu-west-1`), and output format (`json`)
- `make`, `go`, `node` / `npm` available locally

Verify the CLI is working:

```bash
aws sts get-caller-identity --no-cli-pager
```

---

## Option A — Lambda + S3 (recommended, ~$0/month)

### How it works

- The Go binary runs as an AWS Lambda function (charged per millisecond of use,
  free for the first 1 million requests per month — far more than needed here)
- `data.json` is stored in a private S3 bucket instead of the local filesystem
- A Lambda Function URL provides a public HTTPS address with no extra cost

### Step 1 — Set your variables

Edit these to match your preferences, then paste the whole block into your
terminal. Every subsequent command in this section uses these variables.

```bash
export AWS_REGION=eu-west-1          # change to your preferred region
export FUNCTION_NAME=bgpicker
export BUCKET_NAME=bgpicker-state-$(aws sts get-caller-identity \
  --query Account --output text --no-cli-pager)
```

### Step 2 — Create the S3 bucket for state

```bash
aws s3api create-bucket \
  --bucket "$BUCKET_NAME" \
  --region "$AWS_REGION" \
  --create-bucket-configuration LocationConstraint="$AWS_REGION" \
  --no-cli-pager

aws s3api put-public-access-block \
  --bucket "$BUCKET_NAME" \
  --public-access-block-configuration \
    BlockPublicAcls=true,IgnorePublicAcls=true,BlockPublicPolicy=true,RestrictPublicBuckets=true \
  --no-cli-pager
```

> **Note:** If your region is `us-east-1`, omit the
> `--create-bucket-configuration` argument — it is not accepted there.

### Step 3 — Create the IAM role

```bash
# Create the role
aws iam create-role \
  --role-name bgpicker-lambda-role \
  --assume-role-policy-document '{
    "Version": "2012-10-17",
    "Statement": [{
      "Effect": "Allow",
      "Principal": { "Service": "lambda.amazonaws.com" },
      "Action": "sts:AssumeRole"
    }]
  }' \
  --no-cli-pager

# Allow Lambda to write CloudWatch logs
aws iam attach-role-policy \
  --role-name bgpicker-lambda-role \
  --policy-arn arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole \
  --no-cli-pager

# Allow the role to read and write the state file in S3
aws iam put-role-policy \
  --role-name bgpicker-lambda-role \
  --policy-name bgpicker-s3-state \
  --policy-document "{
    \"Version\": \"2012-10-17\",
    \"Statement\": [{
      \"Effect\": \"Allow\",
      \"Action\": [\"s3:GetObject\", \"s3:PutObject\"],
      \"Resource\": \"arn:aws:s3:::${BUCKET_NAME}/data.json\"
    }]
  }" \
  --no-cli-pager
```

IAM changes can take up to 10 seconds to propagate. Continue to the next step
while you wait.

### Step 4 — Build the Lambda zip

```bash
make lambda
```

This cross-compiles the Go binary for Linux/ARM64 (AWS Graviton — the cheapest
Lambda compute option) and packages it as `bgpicker-lambda.zip`.

### Step 5 — Create the Lambda function

```bash
export ACCOUNT_ID=$(aws sts get-caller-identity \
  --query Account --output text --no-cli-pager)

aws lambda create-function \
  --function-name "$FUNCTION_NAME" \
  --runtime provided.al2023 \
  --architectures arm64 \
  --role "arn:aws:iam::${ACCOUNT_ID}:role/bgpicker-lambda-role" \
  --handler bootstrap \
  --zip-file fileb://bgpicker-lambda.zip \
  --timeout 15 \
  --memory-size 128 \
  --environment "Variables={STATE_BUCKET=${BUCKET_NAME}}" \
  --region "$AWS_REGION" \
  --no-cli-pager
```

### Step 6 — Create a public Function URL

```bash
aws lambda create-function-url-config \
  --function-name "$FUNCTION_NAME" \
  --auth-type NONE \
  --region "$AWS_REGION" \
  --no-cli-pager

# Allow unauthenticated access via the Function URL
aws lambda add-permission \
  --function-name "$FUNCTION_NAME" \
  --statement-id FunctionURLAllowPublicAccess \
  --action lambda:InvokeFunctionUrl \
  --principal "*" \
  --function-url-auth-type NONE \
  --region "$AWS_REGION" \
  --no-cli-pager

# Allow direct invocation (required in addition to the URL permission)
aws lambda add-permission \
  --function-name "$FUNCTION_NAME" \
  --statement-id FunctionAllowPublicInvoke \
  --action lambda:InvokeFunction \
  --principal "*" \
  --region "$AWS_REGION" \
  --no-cli-pager
```

The `create-function-url-config` output contains a `FunctionUrl` field — that
is your app's permanent HTTPS address. Share it with everyone.

To look it up again at any time:

```bash
aws lambda get-function-url-config \
  --function-name "$FUNCTION_NAME" \
  --region "$AWS_REGION" \
  --query FunctionUrl \
  --output text \
  --no-cli-pager
```

### Updating the app after code changes

```bash
make deploy FUNCTION_NAME=bgpicker
```

This rebuilds the zip and uploads it in one step.

---

## Option B — EC2 t4g.nano

No code changes required. The binary runs as a plain HTTP server on the instance.

### Step 1 — Create a key pair (if you don't have one)

```bash
aws ec2 create-key-pair \
  --key-name bgpicker-key \
  --query KeyMaterial \
  --output text \
  --no-cli-pager > bgpicker-key.pem

chmod 400 bgpicker-key.pem
```

### Step 2 — Create a security group

```bash
export VPC_ID=$(aws ec2 describe-vpcs \
  --filters Name=isDefault,Values=true \
  --query "Vpcs[0].VpcId" \
  --output text \
  --no-cli-pager)

export SG_ID=$(aws ec2 create-security-group \
  --group-name bgpicker-sg \
  --description "bgpicker web app" \
  --vpc-id "$VPC_ID" \
  --query GroupId \
  --output text \
  --no-cli-pager)

# Allow SSH and app port
aws ec2 authorize-security-group-ingress \
  --group-id "$SG_ID" \
  --protocol tcp --port 22 --cidr 0.0.0.0/0 \
  --no-cli-pager

aws ec2 authorize-security-group-ingress \
  --group-id "$SG_ID" \
  --protocol tcp --port 8080 --cidr 0.0.0.0/0 \
  --no-cli-pager
```

### Step 3 — Launch the instance

```bash
export AMI_ID=$(aws ec2 describe-images \
  --owners amazon \
  --filters \
    "Name=name,Values=al2023-ami-*-arm64" \
    "Name=state,Values=available" \
  --query "sort_by(Images, &CreationDate)[-1].ImageId" \
  --output text \
  --no-cli-pager)

export INSTANCE_ID=$(aws ec2 run-instances \
  --image-id "$AMI_ID" \
  --instance-type t4g.nano \
  --key-name bgpicker-key \
  --security-group-ids "$SG_ID" \
  --count 1 \
  --query "Instances[0].InstanceId" \
  --output text \
  --no-cli-pager)

echo "Instance ID: $INSTANCE_ID"

# Wait until it is running
aws ec2 wait instance-running --instance-ids "$INSTANCE_ID" --no-cli-pager
```

### Step 4 — Allocate a static IP

```bash
export ALLOC_ID=$(aws ec2 allocate-address \
  --query AllocationId \
  --output text \
  --no-cli-pager)

aws ec2 associate-address \
  --instance-id "$INSTANCE_ID" \
  --allocation-id "$ALLOC_ID" \
  --no-cli-pager

export PUBLIC_IP=$(aws ec2 describe-addresses \
  --allocation-ids "$ALLOC_ID" \
  --query "Addresses[0].PublicIp" \
  --output text \
  --no-cli-pager)

echo "Your app will be at http://${PUBLIC_IP}:8080"
```

### Step 5 — Deploy the app

SSH in (wait ~30 seconds after launch for the instance to be ready):

```bash
ssh -i bgpicker-key.pem ec2-user@"$PUBLIC_IP"
```

On the server, run:

```bash
# Install Go
sudo dnf install -y golang git

# Clone and build
git clone https://github.com/YOUR_USERNAME/bgpicker
cd bgpicker
make build

# Create a systemd service so the app restarts automatically
sudo tee /etc/systemd/system/bgpicker.service > /dev/null <<EOF
[Unit]
Description=bgpicker board game picker
After=network.target

[Service]
ExecStart=/home/ec2-user/bgpicker/bgpicker
WorkingDirectory=/home/ec2-user/bgpicker
Restart=always
RestartSec=5
Environment=PORT=8080

[Install]
WantedBy=multi-user.target
EOF

sudo systemctl daemon-reload
sudo systemctl enable --now bgpicker
sudo systemctl status bgpicker
```

### Stopping and starting to save money

Stop the instance when game night is over (you only pay for EBS storage, ~$0.80/month):

```bash
aws ec2 stop-instances \
  --instance-ids "$INSTANCE_ID" \
  --no-cli-pager
```

Start it again before the next session (boots in ~30 seconds, the Elastic IP stays the same):

```bash
aws ec2 start-instances \
  --instance-ids "$INSTANCE_ID" \
  --no-cli-pager
```

### Updating the app

```bash
ssh -i bgpicker-key.pem ec2-user@"$PUBLIC_IP"

cd bgpicker
git pull
make build
sudo systemctl restart bgpicker
```
