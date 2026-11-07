## Part 1: Project Overview and Tech Stack

### Introduction

In this blog series, I'll share my experience building this portfolio website using modern web technologies. This project serves as both a showcase for my work and a practical demonstration of my technical skills in frontend development.

![Portfolio Website Blog Thumbnail](/thumbs/web-thumb.png)

![Portfolio Website Blog Screenshot](/blog-img/portfolio-app/baoopn_web.jpg)

### Tech Stack Overview

For this portfolio website, I chose a powerful and flexible tech stack:

#### Frontend

- **React**: For building a component-based UI
- **TypeScript**: For type safety and improved developer experience
- **TanStack Router**: For efficient client-side routing with type safety
- **Framer Motion**: For smooth animations and transitions
- **Tailwind CSS**: For rapid styling with utility classes



#### Backend

- **Spring Boot**: For building a lightweight API service
- **Spotify API**: For retrieving my currently playing tracks
- **Telegram Bot API**: For sending contact form submissions as messages
- **Docker**: For containerizing the backend service

### Project Structure

I organized the project with a clear separation of concerns:

```plaintext
src/
├── components/     // Reusable UI components
├── routes/         // Page routes
├── blog/           // Markdown blog content
├── styles/         // Global styles
├── utils/          // Utility functions and helpers
└── main.tsx        // Application entry point
```

### Key Features

1. **Responsive Design**: Adapts seamlessly to all screen sizes
2. **Dark Mode**: Automatic theme detection based on browser's theme
3. **Smooth Animations**: Subtle transitions between sections and pages
4. **Markdown Blog**: Dynamic blog content using React Markdown
5. **Integrated Spotify**: Real-time display of my currently playing music

![Spotify Integration Screenshot](/blog-img/portfolio-app/spotify.png)

6. **Contact Form**: Form validation with real-time feedback and Telegram notifications

![Contact Form Screenshot](/blog-img/portfolio-app/contact-form.png) 

![Telegram Screenshot](/blog-img/portfolio-app/telegram.png)

### Development Approach

I built this portfolio with a focus on:

- **Performance**: Optimized bundle size and rendering efficiency
- **Code Quality**: Consistent patterns and reusable components
- **User Experience**: Smooth transitions and intuitive navigation

In the next parts of this series, I'll dive deeper into specific aspects like the animation system, theme implementation, and creating reusable UI components.
