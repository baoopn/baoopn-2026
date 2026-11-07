# Part 4: Integrating Backend Services with React Frontend

## Creating a Seamless Full Stack Experience

In this final part of the series, I'll explain how I integrated the Spring Boot backend services with my React frontend to create a cohesive user experience. I'll focus on two key integrations: the contact form and the Spotify player component.

### Setting Up API Communication

First, I established a clean approach to handle API communication by creating utility constants to manage endpoints:

```tsx
// src/utils/constants.ts
export const API_URL = import.meta.env.VITE_API_URL || "http://localhost:8080";
export const CONTACT_FORM_ENDPOINT = "/api/messages";
export const SPOTIFY_WEBSOCKET_ENDPOINT = "/ws/spotify";
```

Using environment variables for the base URL allows for different configurations in development and production environments.

### Contact Form Integration

The contact form demonstrates a complete frontend-to-backend flow. Here's how I integrated it:

#### Component Architecture

I created reusable form components (`FormInput` and `FormTextarea`) to handle individual form fields, then combined them in the `ContactForm` component:

```tsx
// Form field components with validation states
<FormInput
  label="Name"
  id="name"
  name="name"
  type="text"
  placeholder="John Doe"
  value={messageRequest.name}
  error={errorMessages.name}
  touched={touchedFields.name}
  onChange={(e) => {
    setMessageRequest({ ...messageRequest, name: e.target.value });
    setErrorMessages({ ...errorMessages, name: "" });
  }}
  onInputBlur={() => handleBlur("name")}
/>
```

#### Form State Management

The form uses several state variables to track different aspects of user interaction:

```tsx
// State for form data and validation
const [messageRequest, setMessageRequest] = useState<MessageRequest>(InitialMessageRequest);
const [errorMessages, setErrorMessages] = useState<ErrorMessages>(InitialErrorMessages);
const [touchedFields, setTouchedFields] = useState<Record<string, boolean>>({});
const [alert, setAlert] = useState<Alert>(InitialAlert);
const [isSubmitting, setIsSubmitting] = useState(false);
const [isFormValid, setIsFormValid] = useState(false);
```

I used React's `useEffect` to dynamically validate the form as users type:

```tsx
// Real-time form validation
useEffect(() => {
  const hasName = messageRequest.name.trim().length >= 2;
  const hasValidEmail = validateEmail(messageRequest.contact);
  const hasMessage = messageRequest.message.trim().length >= 10;

  setIsFormValid(hasName && hasValidEmail && hasMessage);
  setAlert(InitialAlert);
}, [messageRequest.name, messageRequest.contact, messageRequest.message]);
```

#### API Integration

The form submission process connects to our Spring Boot backend using Axios:

```tsx
// Handle form submission
const handleSubmit = async (e: React.FormEvent) => {
  e.preventDefault();
  
  // Validate fields...
  
  // If no errors, submit the form
  if (!nameError && !emailError && !messageError) {
    setIsSubmitting(true);
    try {
      const response = await axios.post(
        `${API_URL}${CONTACT_FORM_ENDPOINT}`,
        messageRequest
      );
      if (response.status === HttpStatusCode.Ok) {
        setAlert({
          message: "Message sent successfully!",
          type: "success",
        });
      }
    } catch (error) {
      setAlert({
        message: "An error occurred. Please try again later.",
        type: "error",
      });
    } finally {
      setIsSubmitting(false);
    }
    
    // Reset form...
  }
};
```

This aligns perfectly with our backend `MessageController`, which validates the request and sends it to Telegram.

#### User Experience Enhancements

To create a seamless experience, I added several UX enhancements:

1. **Real-time validation feedback**:
   ```tsx
   const handleBlur = (field: keyof MessageRequest) => {
     setTouchedFields({
       ...touchedFields,
       [field]: true,
     });
     
     // Field-specific validation logic...
     
     setErrorMessages({
       ...errorMessages,
       [field]: error,
     });
   };
   ```

2. **Visual feedback during submission**:
   ```tsx
   <Button
     primary
     type="submit"
     className="w-full"
     disabled={isSubmitting || !isFormValid}
   >
     {isSubmitting ? "Sending..." : "Send Message"}
   </Button>
   ```

3. **Outcome notifications**:
   ```tsx
   {alert.message && (
     <div className={`text-${alert.type === "error" ? "red" : alert.type === "success" ? "green" : "gray"}-500 text-center py-2`}>
       {alert.message}
     </div>
   )}
   ```

The result is a form that validates input in real-time, provides immediate feedback, communicates with the backend, and notifies users of the outcome—all while maintaining a clean, accessible interface.

### Real-time Spotify Integration with WebSockets

For the Spotify integration, I needed to establish a persistent connection to receive real-time updates about my currently playing track.

#### WebSocket Connection Management

I created a custom hook to manage WebSocket connections:

```tsx
// src/hooks/useWebSocket.tsx
import { useEffect, useState, useRef, useCallback } from 'react';

export function useWebSocket<T>(url: string, initialData: T) {
  const [data, setData] = useState<T>(initialData);
  const [error, setError] = useState<string | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  const socketRef = useRef<WebSocket | null>(null);
  
  const connect = useCallback(() => {
    try {
      const socket = new WebSocket(url);
      
      socket.onopen = () => {
        setIsConnected(true);
        setError(null);
        socket.send("currently-playing");
      };
      
      socket.onmessage = (event) => {
        try {
          const parsedData = JSON.parse(event.data);
          setData(parsedData);
        } catch (e) {
          setError("Failed to parse data");
        }
      };
      
      socket.onclose = () => {
        setIsConnected(false);
      };
      
      socket.onerror = () => {
        setError("WebSocket error");
        setIsConnected(false);
      };
      
      socketRef.current = socket;
    } catch (e) {
      setError("Failed to connect");
    }
    
    return () => {
      if (socketRef.current) {
        socketRef.current.close();
      }
    };
  }, [url]);
  
  useEffect(() => {
    const cleanup = connect();
    
    // Reconnect if connection drops
    const interval = setInterval(() => {
      if (socketRef.current?.readyState !== WebSocket.OPEN) {
        cleanup();
        connect();
      }
    }, 5000);
    
    return () => {
      clearInterval(interval);
      cleanup();
    };
  }, [connect]);
  
  return { data, error, isConnected };
}
```

#### Spotify Now Playing Component

I used this hook in the `SpotifyNowPlaying` component to display my currently playing track:

```tsx
// src/components/ui/SpotifyNowPlaying.tsx
import { useWebSocket } from "../../hooks/useWebSocket";
import { API_URL, SPOTIFY_WEBSOCKET_ENDPOINT } from "../../utils/constants";
import { motion } from "framer-motion";

interface SpotifyTrack {
  title: string;
  artist: string;
  albumImageUrl: string;
  songUrl: string;
  playing: boolean;
}

const initialTrack: SpotifyTrack = {
  title: "",
  artist: "",
  albumImageUrl: "",
  songUrl: "",
  playing: false,
};

const SpotifyNowPlaying: React.FC = () => {
  const wsUrl = `ws://${API_URL.replace(/^https?:\/\//, '')}${SPOTIFY_WEBSOCKET_ENDPOINT}`;
  const { data: track, isConnected } = useWebSocket<SpotifyTrack>(wsUrl, initialTrack);
  
  if (!isConnected || (!track.playing && !track.title)) {
    return (
      <div className="bg-[var(--background-lighter)] p-4 rounded-lg shadow-md w-72">
        <p className="text-center text-sm">Not playing anything currently</p>
      </div>
    );
  }
  
  return (
    <div className="bg-[var(--background-lighter)] p-4 rounded-lg shadow-md w-72">
      <div className="flex items-center space-x-4">
        {track.albumImageUrl && (
          <img
            src={track.albumImageUrl}
            alt={`${track.title} album art`}
            className="w-16 h-16 rounded-md"
          />
        )}
        
        <div className="flex-1 min-w-0">
          <a
            href={track.songUrl}
            target="_blank"
            rel="noopener noreferrer"
            className="block truncate font-medium hover:text-[var(--primary-pink)]"
          >
            {track.title}
          </a>
          <p className="text-sm text-[var(--text-color-lighter)] truncate">
            {track.artist}
          </p>
          {track.playing && (
            <div className="flex space-x-1 mt-1">
              {[...Array(3)].map((_, i) => (
                <motion.div
                  key={i}
                  className="w-1 h-3 bg-[var(--primary-pink)] rounded-full"
                  animate={{
                    height: ["3px", "12px", "3px"],
                  }}
                  transition={{
                    repeat: Infinity,
                    duration: 1,
                    delay: i * 0.2,
                  }}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
```

#### Collapsible Integration

To make this feature non-intrusive, I created a `CollapsibleSpotify` component that allows users to toggle the visibility of the player:

```tsx
// src/components/ui/CollapsibleSpotify.tsx
import { useState } from "react";
import SpotifyNowPlaying from "./SpotifyNowPlaying";
import { motion, AnimatePresence } from "framer-motion";
import ActionButton from "./ActionButton";

const CollapsibleSpotify: React.FC = () => {
  const [isVisible, setIsVisible] = useState(false);

  return (
    <div className="fixed bottom-4 right-4 z-40 flex items-end">
      <AnimatePresence>
        {isVisible && (
          <motion.div
            initial={{ opacity: 0, scale: 0.8, x: 100 }}
            animate={{ opacity: 1, scale: 1, x: 0 }}
            exit={{ opacity: 0, scale: 0.8, x: 100 }}
            transition={{ duration: 0.3 }}
            className="mr-4"
          >
            <SpotifyNowPlaying />
          </motion.div>
        )}
      </AnimatePresence>

      <ActionButton
        onClick={() => setIsVisible(!isVisible)}
        primary
        className="p-2 rounded-full !px-2 !py-2 shadow-lg"
        aria-label={isVisible ? "Hide music player" : "Show music player"}
      >
        <svg
          xmlns="http://www.w3.org/2000/svg"
          width="24"
          height="24"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          {isVisible ? (
            <>
              <line x1="18" y1="6" x2="6" y2="18"></line>
              <line x1="6" y1="6" x2="18" y2="18"></line>
            </>
          ) : (
            <>
              <path d="M9 18V5l12-2v13"></path>
              <circle cx="6" cy="18" r="3"></circle>
              <circle cx="18" cy="16" r="3"></circle>
            </>
          )}
        </svg>
      </ActionButton>
    </div>
  );
};
```

This creates a floating music player that can be toggled with a button, showing real-time updates of my Spotify activity.

### Error Handling Across the Stack

To provide a robust user experience, I implemented consistent error handling across both frontend and backend:

1. **Backend validation errors** are returned with appropriate HTTP status codes
2. **Frontend validation** prevents invalid submissions
3. **API error handling** catches and displays user-friendly messages
4. **Connection issues** with WebSockets are handled with automatic reconnection logic

For example, in the contact form submission:

```tsx
try {
  const response = await axios.post(
    `${API_URL}${CONTACT_FORM_ENDPOINT}`,
    messageRequest
  );
  // Success handling
} catch (error) {
  setAlert({
    message:  "An error occurred. Please try again later.",
    type: "error",
  });
} finally {
  setIsSubmitting(false);
}
```

This ensures users always receive appropriate feedback, whether the operation succeeds or fails.

### Security Considerations

When integrating frontend and backend, I implemented several security best practices:

1. **CORS configuration** on the backend to restrict which domains can access the API
2. **Validation on both ends** to prevent malicious data
3. **Environment variables** to keep sensitive information secure
4. **Request throttling** to prevent abuse

### Conclusion

Integrating the Spring Boot backend with the React frontend created a seamless user experience with real-time features. The architecture demonstrates several modern web development patterns:

1. **Component-based design** with clean separation of concerns
2. **Real-time communication** through WebSockets
3. **Robust form handling** with comprehensive validation
4. **Responsive UI elements** that work across devices
5. **Graceful error handling** for a smooth user experience

This integration allows my portfolio to showcase not just static content but also dynamic, interactive features that demonstrate full-stack development capabilities. The combination of Spring Boot's robust backend services with React's flexible frontend components creates a portfolio that's both functional and engaging.