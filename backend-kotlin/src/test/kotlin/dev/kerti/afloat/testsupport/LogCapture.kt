package dev.kerti.afloat.testsupport

import ch.qos.logback.classic.Logger
import ch.qos.logback.classic.spi.ILoggingEvent
import ch.qos.logback.core.read.ListAppender
import org.slf4j.LoggerFactory

// Runs block and returns what type's logger said meanwhile. For a catch whose
// answer the catch-all would give too: the same 500 either way, so only the log
// says which ran.
fun <T> loggedBy(type: Class<*>, block: () -> T): Pair<T, List<String>> {
    val logger = LoggerFactory.getLogger(type) as Logger
    val appender = ListAppender<ILoggingEvent>().apply { start() }
    logger.addAppender(appender)
    try {
        return block() to appender.list.map { it.formattedMessage }
    } finally {
        logger.detachAppender(appender)
    }
}
