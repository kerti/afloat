package dev.kerti.afloat

import dev.kerti.afloat.config.AppConfig
import dev.kerti.afloat.config.ConfigException
import org.springframework.boot.autoconfigure.SpringBootApplication
import org.springframework.boot.runApplication
import kotlin.system.exitProcess

@SpringBootApplication
class AfloatApplication

fun main(args: Array<String>) {
    val config = try {
        AppConfig.load(System.getenv())
    } catch (e: ConfigException) {
        System.err.println(e.message)
        exitProcess(1)
    }
    runApplication<AfloatApplication>(*args)
}
