import { ethers } from "ethers";
import * as consts from './consts'
import { namedAccount, namedAddress } from './accounts'
import * as fs from 'fs';
const path = require("path");

async function bridgeFunds(provider: ethers.providers.Provider, from: ethers.Wallet, ethamount: string, inboxAddress: string): Promise<ethers.providers.TransactionResponse> {
    return from.connect(provider)
        .sendTransaction({
            to: inboxAddress,
            value: ethers.utils.parseEther(ethamount),
            data: "0x0f4d14e9000000000000000000000000000000000000000000000000000082f79cd90000",
        })
}

export const bridgeFundsCommand = {
    command: "bridge-funds",
    describe: "sends funds from l1 to l2",
    builder: {
        ethamount: { string: true, describe: 'amount to transfer (in eth)', default: "10" },
        account: { string: true, describe: 'account name', default: "funnel" },
        privatekey: { string: true },
        inboxAddress: { string: true },
        url: { string: true, default: consts.l1url }
    },
    handler: async (argv: any) => {
        let provider = new ethers.providers.WebSocketProvider(argv.url)

        let wallet: ethers.Wallet
        if (argv.privatekey !== "") {
            wallet = new ethers.Wallet(argv.privatekey)
        } else {
            wallet = namedAccount(argv.account)
        }

        let inboxAddress: string;
        if (argv.inboxAddress !== "") {
            inboxAddress = argv.inboxAddress
        } else {
            inboxAddress = JSON.parse(fs.readFileSync(path.join(consts.configpath, "deployment.json")).toString()).Inbox
        }

        let response = await bridgeFunds(provider, wallet, argv.ethamount, inboxAddress)

        console.log("bridged funds")
        console.log(response)

        provider.destroy()
    }
}

export const sendL1Command = {
    command: "send-l1",
    describe: "sends funds between l1 accounts",
    builder: {
        ethamount: { string: true, describe: 'amount to transfer (in eth)', default: "10" },
        from: { string: true, describe: 'account name', default: "funnel" },
        to: { string: true, describe: 'account name', default: "funnel" },
        data: { string: true, describe: 'data' },
    },
    handler: async (argv: any) => {
        let provider = new ethers.providers.WebSocketProvider(consts.l1url)

        let response = await namedAccount(argv.from).connect(provider)
            .sendTransaction({
                to: namedAddress(argv.to),
                value: ethers.utils.parseEther(argv.ethamount),
                data: argv.data,
            })

        console.log("sent on l1")
        console.log(response)

        provider.destroy()
    }
}

export const sendL2Command = {
    command: "send-l2",
    describe: "sends funds between l2 accounts",
    builder: {
        ethamount: { string: true, describe: 'amount to transfer (in eth)', default: "10" },
        from: { string: true, describe: 'account name', default: "funnel" },
        to: { string: true, describe: 'account name', default: "funnel" },
        data: { string: true, describe: 'data' },
    },
    handler: async (argv: any) => {
        let provider = new ethers.providers.WebSocketProvider(consts.l2url)

        let response = await namedAccount(argv.from).connect(provider)
            .sendTransaction({
                to: namedAddress(argv.to),
                value: ethers.utils.parseEther(argv.ethamount),
                data: argv.data,
            })

        console.log("sent on l2")
        console.log(response)

        provider.destroy()
    }
}
