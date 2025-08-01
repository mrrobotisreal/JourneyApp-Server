# Journey App Server

A Go-based API server for the Journey mobile app, built with Gin, PostgreSQL, Redis, and Firebase Authentication.

## Features

- RESTful API with Gin framework
- Firebase Authentication (email/password and token validation)
- PostgreSQL database with connection pooling
- Redis caching for session management
- Graceful server shutdown
- CORS support for mobile apps

## Prerequisites

- Go 1.24.3 or higher
- PostgreSQL database
- Redis server
- Firebase project with Admin SDK

## Environment Variables

Create a `.env` file or set the following environment variables:

### Database Configuration
```
DATABASE_URL=postgres://mitchwintrow@localhost:5432/journeyapp?sslmode=disable
# OR individual PostgreSQL settings:
POSTGRES_HOST=localhost
POSTGRES_PORT=5432
POSTGRES_USER=mitchwintrow
POSTGRES_PASSWORD=
POSTGRES_DB=journeyapp
POSTGRES_SSLMODE=disable
```

### Redis Configuration
```
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=
REDIS_DB=0
```

### Firebase Configuration
```
FIREBASE_PROJECT_ID=your-firebase-project-id
FIREBASE_SERVICE_ACCOUNT_PATH=/path/to/your/firebase-service-account.json
```

## Installation

1. Clone the repository
2. Install dependencies:
   ```bash
   go mod tidy
   ```

3. Set up your environment variables

4. Build the application:
   ```bash
   go build ./cmd/api
   ```

5. Run the server:
   ```bash
   ./api
   ```

The server will start on port 9091.

## API Endpoints

### Authentication
- `POST /api/v1/auth/login` - User login (email/password or token validation)
- `POST /api/v1/auth/create-account` - Create new user account

### Health Check
- `GET /health` - Server health check

## Database Setup

The application automatically creates all required tables on startup:

### Creating the Database Locally

1. Create the database:
   ```bash
   createdb journeyapp
   ```

2. The following tables are automatically created by the application:
   - **users** - Firebase user information
   - **entries** - Journal entries
   - **locations** - Location data for entries
   - **tags** - Tags associated with entries
   - **images** - Image metadata for entries

### Database Schema

The tables are created with proper relationships and indexes:

```sql
-- Users table
CREATE TABLE users (
    uid VARCHAR(255) PRIMARY KEY,
    display_name VARCHAR(255),
    email VARCHAR(255) UNIQUE NOT NULL,
    token TEXT,
    photo_url TEXT,
    phone_number VARCHAR(20),
    provider_id VARCHAR(100),
    refresh_token TEXT,
    tenant_id VARCHAR(100),
    provider VARCHAR(100),
    email_verified BOOLEAN DEFAULT FALSE,
    phone_number_verified BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Entries table
CREATE TABLE entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_uid VARCHAR(255) NOT NULL REFERENCES users(uid) ON DELETE CASCADE,
    title VARCHAR(500) NOT NULL,
    description TEXT,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Plus locations, tags, and images tables with proper foreign key relationships
```

## Project Structure

```
├── cmd/api/           # Application entry point
├── internal/
│   ├── db/           # Database initialization (PostgreSQL & Redis)
│   ├── firebase/     # Firebase initialization
│   ├── handlers/     # HTTP handlers
│   └── models/       # Data models
│       ├── account/  # User and related models
│       ├── login/    # Login request/response models
│       └── create_account/ # Account creation models
└── README.md
```

## Development

To run in development mode with auto-reloading, you can use tools like `air`:

```bash
go install github.com/cosmtrek/air@latest
air
```
