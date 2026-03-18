# Complex backup script for demonstration
SOURCE=$1
DEST=$2
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
BACKUP_NAME="backup_${TIMESTAMP}.tar.gz"

if [ -z "$SOURCE" ] || [ -z "$DEST" ]; then
    echo "Usage: backup.sh <source> <destination>"
    exit 1
fi

echo "Creating compressed backup of $SOURCE..."
mkdir -p "$DEST"
tar -czf "${DEST}/${BACKUP_NAME}" -C "$(dirname "$SOURCE")" "$(basename "$SOURCE")"

if [ $? -eq 0 ]; then
    echo "Backup created successfully: ${DEST}/${BACKUP_NAME}"
else
    echo "Backup failed!"
    exit 1
fi
