#!/bin/bash

if [[ "$1" == "" ]]
then
  echo "please supply a hostname"
  exit 1
fi
DEVICE_NAME="$1"

if [[ "$PACKET_TOKEN" == "" ]]
then
   echo  "PACKET_TOKEN is not set"
   exit 1
fi

PROJECT_ID="93125c2a-8b78-4d4f-a3c4-7367d6b7cca8"
STORAGE_PLAN="87728148-3155-4992-a730-8d1e6aca8a32"
FACILITY_ID="2b70eb8f-fa18-47c0-aba7-222a842362fd"

DEVICE_ID=$(curl -s -X GET \
    -H "Content-Type: application/json"  \
    -H "X-Auth-Token: $PACKET_TOKEN" \
    https://api.packet.net/projects/${PROJECT_ID}/devices \
| jq -r ".devices[] | select(.hostname==\"$DEVICE_NAME\") | .id")

if [[ "$DEVICE_ID" == "" ]]
then
   echo  "DEVICE_ID not found for $DEVICE_NAME"
   exit 1
fi
echo "DEVICE_ID=$DEVICE_ID"

LOCAL_VOLUME_ID=$(dd if=/dev/urandom bs=128 count=1 2>/dev/null | base64 | tr -d "=+/" | dd bs=32 count=1 2>/dev/null)
echo "LOCAL_VOLUME_ID=$LOCAL_VOLUME_ID"

cat <<EOF > scratch.json
{
  "description": "$LOCAL_VOLUME_ID",
  "facility_id": "$FACILITY_ID",
  "plan_id": "$STORAGE_PLAN",
  "size": "100",
  "locked": "false",
  "billing_cycle": "hourly"
}
EOF

curl -g -X POST -H "Content-Type: application/json" \
   -d @scratch.json  -H "X-Auth-Token: $PACKET_TOKEN" \
   https://api.packet.net/projects/${PROJECT_ID}/storage 2>/dev/null

echo

VOLUME_ID=$(curl -s -H "X-Auth-Token: $PACKET_TOKEN" \
    https://api.packet.net/projects/$PROJECT_ID/storage/ \
    | jq -r ".volumes[] | select(.description==\"$LOCAL_VOLUME_ID\") | .id")

if [[ "$VOLUME_ID" == "" ]]
then
   echo  "VOLUME_ID not found for $UUID"
   exit 1
fi
echo "VOLUME_ID=$VOLUME_ID"



cat <<EOF > scratch.json
{
    "device_id": "$DEVICE_ID"
} 
EOF

curl -g -X POST -d @scratch.json \
   -H "Content-Type: application/json"  \
   -H "X-Auth-Token: $PACKET_TOKEN" \
  https://api.packet.net/storage/${VOLUME_ID}/attachments


# POST https://api.packet.net/storage/102c28c7-47cf-4dfa-82e6-5eb5ae023514/attachments