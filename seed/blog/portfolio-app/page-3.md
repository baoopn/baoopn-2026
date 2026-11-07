## Part 3: Backend Services with Spring Boot

### Introduction to the Backend Architecture

After exploring the frontend in Part 2, let's dive into the backend services that power this portfolio website. I built a lightweight yet robust Spring Boot application to handle the Spotify integration and contact form functionality.

### Tech Stack Overview

For the backend services, I selected these technologies:

- **Spring Boot 3.4**: For creating stand-alone, production-grade services with minimal setup
- **Java 23**: For leveraging the latest language features and performance improvements
- **Maven**: For dependency management and build automation
- **Docker**: For containerization and simplified deployment
- **WebSocket API**: For real-time communication with the frontend

### Spotify Integration

One of the standout features of my portfolio is the real-time display of my currently playing music. This required building a service to interact with Spotify's API.

#### Authentication Flow

The Spotify API uses OAuth 2.0, requiring token-based authentication. You can refer to the [Spotify API documentation](https://developer.spotify.com/documentation/web-api/tutorials/code-flow) for more details on getting access and refresh tokens.

```java
public String getAccessToken() {
    long now = System.currentTimeMillis();

    // Return cached token if still valid
    if (accessToken != null && lastFetched + TOKEN_CACHE_DURATION > now) {
        return accessToken;
    }

    // Token request setup
    SpotifyTokenRequestDTO tokenRequest = new SpotifyTokenRequestDTO();
    tokenRequest.setClientId(clientId);
    tokenRequest.setClientSecret(clientSecret);
    tokenRequest.setRefreshToken(refreshToken);

    // Make request to Spotify token endpoint
    RestTemplate restTemplate = new RestTemplate();
    HttpHeaders headers = new HttpHeaders();
    headers.setContentType(MediaType.APPLICATION_FORM_URLENCODED);
    String auth = clientId + ":" + clientSecret;
    String encodedAuth = Base64.getEncoder().encodeToString(auth.getBytes());
    headers.set("Authorization", "Basic " + encodedAuth);

    MultiValueMap<String, String> body = new LinkedMultiValueMap<>();
    body.add("grant_type", "refresh_token");
    body.add("refresh_token", refreshToken);
    
    // Process response and cache the token
    // ...
}
```

#### Currently Playing Track

To fetch the currently playing track information, I implemented a service that:

1. Checks for a cached response to minimize API calls
2. Requests data from Spotify's currently-playing endpoint
3. Transforms the response into a DTO for the frontend

```java
public SpotifyCurrentlyPlayingTrackDTO getCurrentlyPlayingTrack() {
    // Check cache first
    long now = System.currentTimeMillis();
    if (cachedCurrentlyPlayingTrack != null && 
        lastCurrentlyPlayingFetched + CURRENTLY_PLAYING_CACHE_DURATION > now) {
        return cachedCurrentlyPlayingTrack;
    }
    
    // Get an access token and make the API request
    // ...
    
    // Parse the response
    ObjectMapper objectMapper = new ObjectMapper();
    JsonNode jsonNode = objectMapper.readTree(response.getBody());
    
    SpotifyCurrentlyPlayingTrackDTO track = new SpotifyCurrentlyPlayingTrackDTO();
    track.setAlbumImageUrl(jsonNode.get("item").get("album").get("images").get(0).get("url").asText());
    track.setArtist(
            jsonNode.get("item").get("artists")
                    .findValues("name")
                    .stream()
                    .map(JsonNode::asText)
                    .collect(Collectors.joining(", ")));
    track.setPlaying(jsonNode.get("is_playing").asBoolean());
    track.setSongUrl(jsonNode.get("item").get("external_urls").get("spotify").asText());
    track.setTitle(jsonNode.get("item").get("name").asText());
    
    // Update cache and return
}
```

#### WebSocket Implementation

To provide real-time updates, I implemented a WebSocket handler:

```java
@Component
public class SpotifyWebSocketHandler extends TextWebSocketHandler {

    private final SpotifyService spotifyService;
    private final ObjectMapper objectMapper;

    @Override
    protected void handleTextMessage(WebSocketSession session, TextMessage message) throws Exception {
        String payload = message.getPayload();
        if (payload.equals("currently-playing")) {
            SpotifyCurrentlyPlayingTrackDTO currentlyPlayingTrack = 
                spotifyService.getCurrentlyPlayingTrack();
            session.sendMessage(new TextMessage(
                objectMapper.writeValueAsString(currentlyPlayingTrack)));
        } else if (payload.startsWith("recently-played:")) {
            int limit = Integer.parseInt(payload.split(":")[1]);
            List<SpotifyRecentlyPlayedTrackDTO> recentlyPlayedTracks = 
                spotifyService.getRecentlyPlayedTracks(limit);
            session.sendMessage(new TextMessage(
                objectMapper.writeValueAsString(recentlyPlayedTracks)));
        }
    }
}
```

The WebSocket configuration allows for real-time communication:

```java
@Configuration
@EnableWebSocket
public class WebSocketConfig implements WebSocketConfigurer {

    private final SpotifyWebSocketHandler spotifyWebSocketHandler;

    @Value("${ALLOWED_ORIGINS}")
    private String allowedOrigins;

    @Override
    public void registerWebSocketHandlers(WebSocketHandlerRegistry registry) {
        registry.addHandler(spotifyWebSocketHandler, "/ws/spotify")
                .setAllowedOrigins(allowedOrigins.split(","));
    }
}
```

### Contact Form Integration

The contact form on the portfolio website sends messages directly to my Telegram account using Telegram Bot API. You can refer to [Telegram Bot API documentation](https://core.telegram.org/bots/api) and [Telegram Bot Tutorial](https://core.telegram.org/bots/tutorial) and for more details on setting up a bot for sending and receiving message.

#### Message Service

I built a service to validate and process contact form submissions:

```java
@Service
public class MessageService {
    @Value("${TELEGRAM_BOT_TOKEN}")
    private String botToken;

    @Value("${TELEGRAM_CHAT_ID}")
    private String chatId;

    public boolean sendTelegramMessage(String name, String contact, String subject, String message,
            String contactType) {
        // Create formatted message
        String messageText = String.format(
            Constants.TELEGRAM_MESSAGE_FORMAT,
            escapeMarkdown(name),
            contactType,
            escapeMarkdown(contact),
            escapeMarkdown(subject),
            escapeMarkdown(message)
        );

        // Prepare HTTP request
        String url = Constants.TELEGRAM_API_BASE_URL + botToken + Constants.TELEGRAM_SEND_MESSAGE_ENDPOINT;
        RestTemplate restTemplate = new RestTemplate();
        HttpHeaders headers = new HttpHeaders();
        headers.setContentType(MediaType.APPLICATION_JSON);

        // Create request body
        Map<String, Object> requestBody = new HashMap<>();
        requestBody.put("chat_id", chatId);
        requestBody.put("text", messageText);
        requestBody.put("parse_mode", "MarkdownV2");

        // Send message to Telegram
        HttpEntity<Map<String, Object>> entity = new HttpEntity<>(requestBody, headers);
        try {
            ResponseEntity<String> response = restTemplate.postForEntity(url, entity, String.class);
            return response.getStatusCode() == HttpStatus.OK;
        } catch (RestClientException e) {
            return false;
        }
    }
    
    // Helper methods for contact validation and markdown escaping
    // ...
}
```

#### Message Controller

The controller handles incoming requests from the frontend:

```java
@RestController
public class MessageController {
    private final MessageService messageService;
    private static final Logger logger = LoggerFactory.getLogger(MessageController.class);

    @PostMapping("/api/messages")
    public ResponseEntity<Response> sendMessage(@RequestBody MessageRequest request) {
        if (request.getName() == null || request.getName().trim().isEmpty()) {
            return new ResponseEntity<>(new Response("Name is required", false), HttpStatus.BAD_REQUEST);
        }

        if (request.getContact() == null || request.getContact().trim().isEmpty()) {
            return new ResponseEntity<>(new Response("Contact information is required", false), 
                HttpStatus.BAD_REQUEST);
        }

        if (request.getMessage() == null || request.getMessage().trim().isEmpty()) {
            return new ResponseEntity<>(new Response("Message is required", false), 
                HttpStatus.BAD_REQUEST);
        }

        String contactType = messageService.determineContactType(request.getContact());
        boolean success = messageService.sendTelegramMessage(
            request.getName(),
            request.getContact(),
            request.getSubject(),
            request.getMessage(),
            contactType
        );

        if (success) {
            return new ResponseEntity<>(new Response("Message sent successfully", true), 
                HttpStatus.OK);
        } else {
            return new ResponseEntity<>(
                new Response("Failed to send message. Please try again later.", false),
                HttpStatus.INTERNAL_SERVER_ERROR
            );
        }
    }
}
```

### Cross-Origin Resource Sharing (CORS)

To allow the frontend to communicate with the backend, I implemented a CORS configuration:

```java
@Configuration
public class CorsConfig implements WebMvcConfigurer {

    @Value("${ALLOWED_ORIGINS}")
    private String allowedOrigins;

    @Override
    public void addCorsMappings(CorsRegistry registry) {
        registry.addMapping("/**")
                .allowedOrigins(allowedOrigins.split(","))
                .allowedMethods("GET", "POST", "PUT", "DELETE", "OPTIONS")
                .allowedHeaders("*")
                .allowCredentials(true);
    }
}
```

### Error Handling

I implemented a global exception handler to provide consistent error responses:

```java
@RestControllerAdvice
public class GlobalExceptionHandler {

    @ExceptionHandler(SpotifyServiceException.class)
    public ResponseEntity<ErrorResponse> handleSpotifyServiceException(SpotifyServiceException ex) {
        ErrorResponse errorResponse = new ErrorResponse(
            ex.getMessage(), HttpStatus.INTERNAL_SERVER_ERROR.value());
        return new ResponseEntity<>(errorResponse, HttpStatus.INTERNAL_SERVER_ERROR);
    }

    @ExceptionHandler(IllegalArgumentException.class)
    public ResponseEntity<ErrorResponse> handleIllegalArgumentException(IllegalArgumentException ex) {
        ErrorResponse errorResponse = new ErrorResponse(
            ex.getMessage(), HttpStatus.BAD_REQUEST.value());
        return new ResponseEntity<>(errorResponse, HttpStatus.BAD_REQUEST);
    }

    // Additional handlers...
}
```

### Docker Containerization

For deployment simplicity, I containerized the application using Docker:

```dockerfile
FROM openjdk:24-slim
WORKDIR /app

# Copy Maven Wrapper files
COPY .mvn/ .mvn/
COPY mvnw mvnw
COPY mvnw.cmd mvnw.cmd
COPY pom.xml .

# Make mvnw executable
RUN chmod +x mvnw

# Download dependencies (this layer can be cached unless pom.xml changes)
RUN ./mvnw dependency:go-offline -B

# Copy the project source
COPY src ./src
COPY .env .

# Build the application
RUN ./mvnw package -DskipTests

# Copy the JAR to WORKDIR
RUN cp target/*.jar app.jar

# Expose the port the app runs on
EXPOSE 8080

# Command to run the application
ENTRYPOINT ["java", "-jar", "/app/app.jar"]
```

I also created a docker-compose.yml file to orchestrate services:

```yaml
services:
  my-portfolio-be:
    image: my-portfolio-be
    container_name: my-portfolio-be-container
    build: .
    restart: unless-stopped
    ports:
      - "8080:8080"

  telegram-bot:
    build: ./telegram_bot
    image: telegram-bot
    container_name: telegram-bot-container
    restart: unless-stopped
    env_file:
      - ./telegram_bot/.env
```

### Environment Configuration

To keep sensitive information secure, I used environment variables stored in a .env file:

```
# CORS Allowed Origins
ALLOWED_ORIGINS=http://localhost:3000,https://baoopn.com

# Spotify API
SPOTIFY_CLIENT_ID=****
SPOTIFY_CLIENT_SECRET=****
SPOTIFY_REFRESH_TOKEN=****

# Telegram Bot
TELEGRAM_BOT_TOKEN=****
TELEGRAM_CHAT_ID=****
```

These variables are loaded by Spring Boot using:

```properties
spring.config.import=optional:file:.env[.properties]
```

### Conclusion

The Spring Boot backend for my portfolio website demonstrates several important concepts:

1. **API Integration**: Connecting to third-party services (Spotify and Telegram)
2. **Real-time Communication**: Using WebSockets for live updates
3. **Security**: Protecting sensitive credentials with environment variables
4. **Containerization**: Simplifying deployment with Docker
5. **Error Handling**: Creating a robust system that gracefully handles failures

This lightweight backend architecture works perfectly for a portfolio site, providing the necessary functionality without unnecessary complexity.

In the final part of this series, I'll explain how I integrated these backend services with the React frontend, including the implementation of WebSocket connections, handling API requests, and creating a seamless user experience across the full stack.