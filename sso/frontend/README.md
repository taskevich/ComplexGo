# SSO frontend

Заготовка React + Vite. Компонент `src/App.jsx` возвращает `null`:
интерфейс и обращения к API пока не реализованы.

## Структура

```text
frontend/
├── public/
│   ├── images/
│   └── index.html
├── src/
│   ├── App.jsx
│   └── main.jsx
├── .env
├── .gitignore
├── .dockerignore
├── eslint.config.js
├── package.json
├── package-lock.json
├── vite.config.js
├── Dockerfile
└── README.md
```

Все статические файлы — изображения, иконки, шрифты — размещаются в
`public`. Ссылки на них начинаются от `/`, например `/images/logo.png`.
`public/index.html` — входная HTML-страница приложения.

Vite использует `public` как корень сайта, а `/src` — как алиас на исходный
код. При сборке HTML обрабатывается Vite, остальные статические файлы
копируются в `dist`. В режиме разработки HTML также проходит обработку
Vite, включая обновление React-компонентов.

## Локальная разработка

Из директории `frontend`:

```bash
npm ci
npm run dev
```

Сборка: `npm run build`. Просмотр сборки: `npm run preview`.
Проверка кода: `npm run lint`.

Переменные окружения задаются в `.env` в корне `frontend` с префиксом
`VITE_`. Этот файл не хранится в Git. Значения `VITE_*` попадают в клиентскую
сборку, поэтому не предназначены для секретов.

## Docker

Из директории `frontend`:

```bash
docker build -t sso-frontend .
docker run --rm -p 127.0.0.1:3000:80 sso-frontend
```

Сборка выполняется внутри Node.js-контейнера, результат раздаёт Nginx.
Приложение доступно на http://localhost:3000. Локальный `.env` в образ
не копируется; заготовка не требует переменных для Docker-сборки.

Из корня `sso` весь проект запускается командой `docker compose up --build`
после настройки корневого `.env` для backend.
