# coding=utf-8

import logging
import json
import multiprocessing
import optparse
import os
import signal
import sys
import time

sys.path = [os.path.dirname(__file__)] + sys.path

try:
    from setproctitle import getproctitle, setproctitle
except ImportError:
    setproctitle = None

# Path Fix
sys.path.append(
    os.path.abspath(
        os.path.join(
            os.path.dirname(__file__), "../")))

from diamond.utils.classes import initialize_collector
from diamond.utils.classes import load_collectors

from diamond.utils.scheduler import collector_process

from diamond.utils.signals import signal_to_exception
from diamond.utils.signals import SIGHUPException


def load_config(configfile):
    configfile_path = os.path.abspath(configfile)
    with open(configfile_path, "r") as f:
        return json.load(f)


class Server(object):
    """
    Server class loads and starts Handlers and Collectors
    """

    def __init__(self, configfile):
        # Initialize Logging
        self.log = logging.getLogger('diamond')
        # Initialize Members
        self.configfile = configfile
        self.config = None
        self.modules = {}

        # We do this weird process title swap around to get the sync manager
        # title correct for ps
        if setproctitle:
            oldproctitle = getproctitle()
            setproctitle('%s - SyncManager' % getproctitle())
        self.manager = multiprocessing.Manager()
        if setproctitle:
            setproctitle(oldproctitle)

    def run(self):
        """
        Load handler and collector classes and then start collectors
        """

        ########################################################################
        # Config
        ########################################################################
        self.config = load_config(self.configfile)

        collectors = load_collectors(self.config['diamond_collectors_path'])

        ########################################################################
        # Signals
        ########################################################################

        signal.signal(signal.SIGHUP, signal_to_exception)

        ########################################################################

        while True:
            try:
                active_children = multiprocessing.active_children()
                running_processes = []
                for process in active_children:
                    running_processes.append(process.name)
                running_processes = set(running_processes)

                ##############################################################
                # Collectors
                ##############################################################

                running_collectors = []
                for collector, config in self.config['diamond_collectors'].iteritems():
                    # Inject keys to collector's configuration.
                    #
                    # "enabled" is requied to be compatible with
                    # diamond configuration. There are collectors that
                    # check if they are enabled.
                    #
                    # We use "fullerite_port" in collectors to connect
                    # to the running fullerite instance.
                    config['enabled'] = True
                    config['fullerite_port'] = self.config['fullerite_port']

                    running_collectors.append(collector)
                running_collectors = set(running_collectors)
                self.log.debug("Running collectors: %s" % running_collectors)

                # Collectors that are running but shouldn't be
                for process_name in running_processes - running_collectors:
                    if 'Collector' not in process_name:
                        continue
                    for process in active_children:
                        if process.name == process_name:
                            process.terminate()

                collector_classes = dict(
                    (cls.__name__.split('.')[-1], cls)
                    for cls in collectors.values()
                )

                for process_name in running_collectors - running_processes:
                    # To handle running multiple collectors concurrently, we
                    # split on white space and use the first word as the
                    # collector name to spin
                    collector_name = process_name.split()[0]

                    if 'Collector' not in collector_name:
                        continue

                    if collector_name not in collector_classes:
                        self.log.error('Can not find collector %s',
                                       collector_name)
                        continue

                    collector = initialize_collector(
                        collector_classes[collector_name],
                        name=process_name,
                        config=self.config,
                        handlers=[])

                    if collector is None:
                        self.log.error('Failed to load collector %s',
                                       process_name)
                        continue

                    # Splay the loads
                    time.sleep(1)

                    process = multiprocessing.Process(
                        name=process_name,
                        target=collector_process,
                        args=(collector, self.log)
                        )
                    process.daemon = True
                    process.start()

                ##############################################################

                time.sleep(1)

            except SIGHUPException:
                self.log.info('Reloading state due to HUP')
                self.config = load_config(self.configfile)
                collectors = load_collectors(
                    self.config['diamond_collectors_path'])


def main():
    parser = optparse.OptionParser()
    parser.add_option("-c", "--config-file", dest="config_file",
                      help="Fullerite configuration file", metavar="FILE")
    parser.add_option("-d", "--debug",
                      action="store_true", dest="debug", default=False,
                      help="Enable debug logs")
    (options, args) = parser.parse_args()

    if options.debug:
        logging.basicConfig(level=logging.DEBUG)
    else:
        logging.basicConfig(level=logging.INFO)

    Server(options.config_file).run()

if __name__ == "__main__":
    main()
