#!/bin/bash
set -e

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


VOLUME_HREF=$(curl -s \
    -H "X-Auth-Token: $PACKET_TOKEN" \
     https://api.packet.net/devices/$DEVICE_ID \
     | jq -r .volumes[0].href )

if [[ "$VOLUME_HREF" == "" ]]
then
   echo  "No volume found for device $DEVICE_NAME $DEVICE_ID"
   exit 1
fi

VOLUME_ID=$(echo $VOLUME_HREF | sed -e 's/.*\///')

ATTACHMENT_ID=$(curl -s -X GET \
        -H "Content-Type: application/json"  \
        -H "X-Auth-Token: $PACKET_TOKEN" \
        https://api.packet.net/storage/$VOLUME_ID/attachments  \
    | jq -r ".attachments[] |  select(.device.href==\"/devices/${DEVICE_ID}\") | select(.volume.href==\"${VOLUME_HREF}\") | .id ")

if [[ "$ATTACHMENT_ID" == "" ]]
then
   echo  "No attachment found for device $DEVICE_ID and volume reference ${VOLUME_HREF}"
   exit 1
fi
echo "ATTACHMENT_ID=$ATTACHMENT_ID"

# Delete them

RESULT=$(curl -s -X DELETE -H "X-Auth-Token: $PACKET_TOKEN" https://api.packet.net/storage/attachments/$ATTACHMENT_ID)
if [[ "$RESULT" != "" ]]
then
   echo  "errr detaching, retry is possible, $RESULT"
   exit 1
fi

RESULT=$(curl -s -X DELETE -s -H "X-Auth-Token: $PACKET_TOKEN" https://api.packet.net/storage/$VOLUME_ID)
if [[ "$RESULT" != "" ]]
then
   echo  "error deleting volume, $RESULT"
   exit 1
fi


