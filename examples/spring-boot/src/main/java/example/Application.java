package example;

import java.util.Map;
import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.context.annotation.Profile;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

@SpringBootApplication
public class Application {
    public static void main(String[] args) {
        SpringApplication.run(Application.class, args);
    }
}

@RestController
class GreetingController {
    @GetMapping(value = "/greeting", produces = "application/json")
    Map<String, Object> greeting() {
        return Map.of("greeting", "hello", "version", 1);
    }
}

@RestController
@Profile("extra")
class ExtraController {
    @GetMapping(value = "/extra", produces = "application/json")
    Map<String, Object> extra() {
        return Map.of("enabled", true);
    }
}
