plugins {
    kotlin("jvm") version "2.3.21"
    kotlin("plugin.spring") version "2.3.21"
    id("org.springframework.boot") version "4.1.1"
    id("io.spring.dependency-management") version "1.1.7"
    kotlin("plugin.jpa") version "2.3.21"
    id("org.openapi.generator") version "7.25.0"
    jacoco
}

group = "dev.kerti.afloat"
version = "0.0.1-SNAPSHOT"

java {
    toolchain {
        languageVersion = JavaLanguageVersion.of(21)
    }
}

repositories {
    mavenCentral()
}

dependencies {
    implementation("org.springframework.boot:spring-boot-starter-actuator")
    implementation("org.springframework.boot:spring-boot-starter-data-jpa")
    implementation("org.springframework.boot:spring-boot-starter-flyway")
    implementation("org.springframework.boot:spring-boot-starter-security")
    implementation("org.springframework.boot:spring-boot-starter-validation")
    implementation("org.springframework.boot:spring-boot-starter-webmvc")
    implementation("org.flywaydb:flyway-database-postgresql")
    // Spring Security's Argon2PasswordEncoder delegates to Bouncy Castle and is
    // not usable without it: the starter does not pull it in, so every hash and
    // verify throws NoClassDefFoundError at runtime without this line.
    implementation("org.bouncycastle:bcprov-jdk18on:1.83")
    implementation("org.jetbrains.kotlin:kotlin-reflect")
    implementation("tools.jackson.module:jackson-module-kotlin")
    runtimeOnly("org.postgresql:postgresql")
    testImplementation("org.springframework.boot:spring-boot-starter-actuator-test")
    testImplementation("org.springframework.boot:spring-boot-starter-data-jpa-test")
    testImplementation("org.springframework.boot:spring-boot-starter-flyway-test")
    testImplementation("org.springframework.boot:spring-boot-starter-security-test")
    testImplementation("org.springframework.boot:spring-boot-starter-validation-test")
    testImplementation("org.springframework.boot:spring-boot-starter-webmvc-test")
    testRuntimeOnly("org.junit.platform:junit-platform-launcher")

    val kotestVersion = "6.2.5"

    testImplementation("io.kotest:kotest-runner-junit5:$kotestVersion")
    testImplementation("io.kotest:kotest-assertions-core:$kotestVersion")
    testImplementation("io.kotest:kotest-extensions-spring:$kotestVersion")
    testImplementation("org.testcontainers:testcontainers-postgresql")
}

val openApiOut = layout.buildDirectory.dir("generated/openapi")

tasks.named<org.openapitools.generator.gradle.plugin.tasks.GenerateTask>("openApiGenerate") {
    generatorName.set("kotlin-spring")
    inputSpec.set(project.file("../contract/openapi.yaml").path)
    outputDir.set(openApiOut.get().asFile.absolutePath)
    apiPackage.set("dev.kerti.afloat.api")
    modelPackage.set("dev.kerti.afloat.api.model")
    generateApiTests.set(false)
    generateModelTests.set(false)
    generateApiDocumentation.set(false)
    generateModelDocumentation.set(false)
    configOptions.set(
        mapOf(
            "interfaceOnly" to "true",
            "useTags" to "true",
            "useSpringBoot4" to "true",
            "useJackson3" to "true",
            "requestMappingMode" to "api_interface",
            "documentationProvider" to "none",
            "annotationLibrary" to "none",
            "exceptionHandler" to "false",
            "useSwaggerUI" to "false",
            "gradleBuildFile" to "false",
        )
    )
}

sourceSets["main"].kotlin.srcDir(openApiOut.map { it.dir("src/main/kotlin") })

tasks.named("compileKotlin") {
    dependsOn("openApiGenerate")
}

kotlin {
    compilerOptions {
        freeCompilerArgs.addAll("-Xjsr305=strict", "-Xannotation-default-target=param-property")
    }
}

allOpen {
    annotation("jakarta.persistence.Entity")
    annotation("jakarta.persistence.MappedSuperclass")
    annotation("jakarta.persistence.Embeddable")
}

tasks.withType<Test> {
    useJUnitPlatform()

    // Argon2id is configured at m=19456 KiB and that memory is allocated per
    // hash in flight, so LoginConcurrencySpec's 30 parallel logins want ~570
    // MiB of live heap at once - more than Gradle's 512 MiB default. Without
    // this the suite fails with OutOfMemoryError on a machine with enough cores
    // to genuinely overlap the hashes, and passes on CI's 4-core runner, which
    // is the worst shape a flake can have. This makes the test runnable; it
    // does not fix the product exposure it demonstrates - see issue #33.
    maxHeapSize = "3g"
}

// XML only: the HTML report is for a human at a terminal, and nothing here
// reads it. The report covers the generated API sources too; what is and is not
// counted is decided once, in codecov.yml's ignore list, rather than twice.
tasks.jacocoTestReport {
    dependsOn(tasks.test)
    reports {
        xml.required = true
        html.required = false
    }
}

// Coverage is part of `check`, which is what `make check-kotlin` and CI run, so
// the profile exists without a second invocation of the suite.
tasks.check {
    dependsOn(tasks.jacocoTestReport)
}
