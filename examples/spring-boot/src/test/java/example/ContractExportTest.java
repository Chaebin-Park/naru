package example;

import com.fasterxml.jackson.databind.ObjectMapper;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.TreeMap;
import org.junit.jupiter.api.Test;
import static org.junit.jupiter.api.Assertions.assertNotEquals;
import org.springframework.web.servlet.mvc.method.RequestMappingInfo;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.boot.test.autoconfigure.web.servlet.AutoConfigureMockMvc;
import org.springframework.boot.test.context.SpringBootTest;
import org.springframework.test.context.ActiveProfiles;
import org.springframework.test.web.servlet.MockMvc;
import org.springframework.test.web.servlet.request.MockMvcRequestBuilders;
import org.springframework.web.servlet.mvc.method.annotation.RequestMappingHandlerMapping;

@SpringBootTest
@AutoConfigureMockMvc
@ActiveProfiles("contract")
class ContractExportTest extends ContractExporter {
    @Override String profile() { return "contract"; }
}

abstract class ContractExporter {
    @Autowired MockMvc mvc;
    @Autowired RequestMappingHandlerMapping mappings;
    @Autowired ObjectMapper json;
    @Autowired org.springframework.core.env.Environment environment;

    abstract String profile();

    @Test
    void exportObservedContracts() throws Exception {
        if (!java.util.Arrays.equals(environment.getActiveProfiles(), new String[] {profile()})) {
            throw new IllegalStateException("Unexpected active profile set");
        }
        String output = System.getProperty("naru.output");
        if (output == null || output.isBlank()) {
            throw new IllegalStateException("Use the pack adapter to supply a fresh naru.output directory");
        }
        List<Map<String, Object>> routes = new ArrayList<>();
        mappings.getHandlerMethods().forEach((mapping, handler) -> {
            if (!handler.getBeanType().getPackageName().equals("example")) return;
            Map<String, Object> value = routeValue(mapping);
            String id = profile() + ":" + handler.getBeanType().getName() + "/" + handler.getMethod().getName();
            routes.add(Map.of("id", id, "value", value));
        });
        routes.sort(java.util.Comparator.comparing(item -> (String) item.get("id")));
        List<Map<String, Object>> api = new ArrayList<>();
        // This is an explicit synthetic probe allowlist, not arbitrary response capture.
        for (String path : List.of("/greeting", "/extra")) {
            var response = mvc.perform(MockMvcRequestBuilders.get(path)).andReturn().getResponse();
            Map<String, Object> value = new TreeMap<>();
            value.put("profile", profile());
            value.put("status", response.getStatus());
            value.put("content_type", response.getContentType() == null ? "" : response.getContentType());
            String body = response.getContentAsString();
            value.put("body", body.isEmpty() ? null : json.readTree(body));
            api.add(Map.of("id", profile() + ":GET:" + path, "value", value));
        }
        Path directory = Path.of(output);
        Files.writeString(directory.resolve(profile() + "-routes.json"), json.writeValueAsString(Map.of("schema", 1, "items", routes)));
        Files.writeString(directory.resolve(profile() + "-api.json"), json.writeValueAsString(Map.of("schema", 1, "items", api)));
    }

    private Map<String, Object> routeValue(RequestMappingInfo mapping) {
        Map<String, Object> value = new TreeMap<>();
        value.put("profile", profile());
        value.put("paths", mapping.getPatternValues().stream().sorted().toList());
        value.put("methods", mapping.getMethodsCondition().getMethods().stream().map(Enum::name).sorted().toList());
        value.put("consumes", mapping.getConsumesCondition().getExpressions().stream().map(Object::toString).sorted().toList());
        value.put("produces", mapping.getProducesCondition().getExpressions().stream().map(Object::toString).sorted().toList());
        // Hash conditions instead of exporting potential literal header/parameter values.
        value.put("conditions", sha256(mapping.getHeadersCondition() + "\n" + mapping.getParamsCondition()));
        return value;
    }

    @Test
    void changedNegatedMediaConditionsRemainVisible() {
        var xml = RequestMappingInfo.paths("/example").consumes("!application/xml").produces("!text/plain").build();
        var jsonType = RequestMappingInfo.paths("/example").consumes("!application/json").produces("!text/html").build();
        assertNotEquals(routeValue(xml), routeValue(jsonType));
    }

    private static String sha256(String value) {
        try {
            byte[] bytes = java.security.MessageDigest.getInstance("SHA-256").digest(value.getBytes(java.nio.charset.StandardCharsets.UTF_8));
            return java.util.HexFormat.of().formatHex(bytes);
        } catch (java.security.NoSuchAlgorithmException impossible) {
            throw new IllegalStateException(impossible);
        }
    }
}
