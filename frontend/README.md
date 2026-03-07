# Frontend (React + Vite)

## Requisitos
- Node.js 18+

## Ejecutar
1. Instalar dependencias:
   npm install
2. Copiar variables de entorno:
   cp .env.example .env
3. Iniciar en desarrollo:
   npm run dev

## Login con backend
El botón de login abre:
- `GET {VITE_API_BASE_URL}/auth/lastfm/start`

Después del auth en Last.fm, el backend debe redirigir al frontend usando `FRONTEND_URL` y enviará en el hash:
- `token`
- `user_id`
- `username`

Este frontend los guarda en `localStorage`.
