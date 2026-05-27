# Security Policy

## Supported Versions

| Version | Supported          |
| ------- | ------------------ |
| latest  | Yes                |

## Reporting a Vulnerability

If you discover a security vulnerability in Lark, please report it responsibly.

**Do not open a public GitHub issue for security vulnerabilities.**

Instead, send an email to **security@graycodeai.com** with:

- A description of the vulnerability
- Steps to reproduce the issue
- The potential impact
- Any suggested fix (optional)

### What to expect

- **Acknowledgment** within 48 hours
- **Status update** within 5 business days with an initial assessment
- **Fix timeline** communicated once the issue is confirmed
- **Credit** in the release notes (unless you prefer to remain anonymous)

## Security Best Practices for Operators

- Always set a strong `LARK_JWT_SECRET` (use `openssl rand -hex 32`)
- Use TLS in production (`LARK_TLS_CERT` and `LARK_TLS_KEY`)
- Set `LARK_CORS_ORIGIN` to your specific domain, not `*`
- Restrict `LARK_RATE_LIMIT` appropriately for your traffic
- Use S3 storage for file uploads in production instead of local storage
- Keep the Docker image and Go dependencies up to date
- Review webhook secrets regularly

## Disclosure Policy

We follow coordinated disclosure. We ask that reporters give us reasonable time
to address a vulnerability before making it public.
