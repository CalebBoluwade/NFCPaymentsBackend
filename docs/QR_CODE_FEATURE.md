# QR Code Payment Feature

## Overview
The QR code feature allows users to generate and process QR codes for payments.

## Endpoints

### 1. Generate QR Code
**POST** `/api/v1/qr/generate`

Generates a QR code with payment information.

**Request:**
```json
{
  "amount": 5000
}
```

**Response:**
```json
{
  "success": true,
  "qrCode": "eyJhbW91bnQiOjUwMDAsIm5vbmNlIjoiQzR5cUZVT25SU3dHVEZaeEZIa3dLUT09IiwidGltZXN0YW1wIjoxNzY4OTA3Mjk3LCJ1c2VySWQiOiI0In0=",
  "qrImage": "iVBORw0KGgoAAAANSUhEUgAAAQAAAAEACAYAAABccqhmAA..."
}
```

- `qrCode`: Base64-encoded payment data (used for processing)
- `qrImage`: Base64-encoded PNG image of the QR code (256x256px)

### 2. Process QR Code
**POST** `/api/v1/qr/process`

Processes a scanned QR code and returns payment information.

**Request:**
```json
{
  "qrData": "eyJhbW91bnQiOjUwMDAsIm5vbmNlIjoiQzR5cUZVT25SU3dHVEZaeEZIa3dLUT09IiwidGltZXN0YW1wIjoxNzY4OTA3Mjk3LCJ1c2VySWQiOiI0In0="
}
```

**Response:**
```json
{
  "success": true,
  "data": {
    "userId": "4",
    "amount": 5000,
    "timestamp": 1768907297,
    "nonce": "C4yqFUOnRSwGTFZxFHkwKQ=="
  }
}
```

## Features

- **QR Code Generation**: Creates a scannable QR code image (PNG, 256x256px)
- **Data Encoding**: Payment data is base64-encoded and includes:
  - User ID
  - Amount
  - Timestamp
  - Cryptographic nonce for uniqueness
- **Expiration**: QR codes expire after 5 minutes
- **One-time Use**: QR codes are deleted after processing
- **Redis Storage**: Temporary storage for validation

## Usage Example

### Display QR Code in Frontend
```javascript
// Generate QR code
const response = await fetch('/api/v1/qr/generate', {
  method: 'POST',
  headers: {
    'Authorization': 'Bearer <token>',
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({ amount: 5000 })
});

const { qrImage } = await response.json();

// Display as image
document.getElementById('qr').src = `data:image/png;base64,${qrImage}`;
```

### Process Scanned QR Code
```javascript
// After scanning QR code
const response = await fetch('/api/v1/qr/process', {
  method: 'POST',
  headers: {
    'Authorization': 'Bearer <token>',
    'Content-Type': 'application/json'
  },
  body: JSON.stringify({ qrData: scannedData })
});

const { data } = await response.json();
console.log('Payment amount:', data.amount);
console.log('From user:', data.userId);
```

## Security

- QR codes contain cryptographic nonces to prevent replay attacks
- Codes expire after 5 minutes
- One-time use only (deleted after processing)
- Requires authentication for both generation and processing
- Data is validated before processing
